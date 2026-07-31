// Package s3 implements a hyperproviders HostingEngine backed by an
// S3-compatible object store (AWS S3, Cloudflare R2, MinIO, DigitalOcean
// Spaces, etc.).
package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	hosting "github.com/herdworks/hyperproviders-go"
	"golang.org/x/sync/errgroup"
)

const (
	Kind = "s3"

	AddressingStylePath    = "path"
	AddressingStyleVirtual = "virtual"

	ConditionalModeNone = ""
	ConditionalModeETag = "etag"

	defaultRegion          = "auto"
	defaultAddressingStyle = AddressingStylePath

	// DefaultWorkers bounds concurrent S3 object uploads when workers is omitted.
	DefaultWorkers = 10

	secretAccessKeyID     = "access_key_id"
	secretSecretAccessKey = "secret_access_key"
)

func init() {
	hosting.RegisterEngine(Kind, Engine{})
}

// Engine deploys files to an S3-compatible bucket.
type Engine struct{}

type settings struct {
	Bucket          string `json:"-" yaml:"-"`
	Region          string `json:"-" yaml:"-"`
	AddressingStyle string `json:"-" yaml:"-"`
	UsePathStyle    bool   `json:"-" yaml:"-"`
	CacheControl    string `json:"-" yaml:"-"`
	ConditionalMode string `json:"-" yaml:"-"`
	Workers         int    `json:"-" yaml:"-"`
}

// ProviderConfig holds S3-specific settings from the provider config object.
type ProviderConfig struct {
	Bucket          string `json:"bucket"`
	Region          string `json:"region,omitempty"`
	AddressingStyle string `json:"addressing_style,omitempty"`
	CacheControl    string `json:"cache_control,omitempty"`
	ConditionalMode string `json:"conditional_mode,omitempty"`
	Workers         int    `json:"workers,omitempty"`
}

func newSettings(provider *hosting.HostingProvider) (*settings, error) {
	cfg := &settings{}
	if len(provider.Config) > 0 {
		var cfg ProviderConfig
		if err := provider.DecodeConfig(&cfg); err != nil {
			return nil, fmt.Errorf("s3: %w", err)
		}
		if _, ok := provider.Config["workers"]; ok && cfg.Workers < 1 {
			return nil, errors.New("s3: workers must be at least 1")
		}
		settings := &settings{
			Bucket:          cfg.Bucket,
			Region:          cfg.Region,
			AddressingStyle: cfg.AddressingStyle,
			CacheControl:    cfg.CacheControl,
			ConditionalMode: cfg.ConditionalMode,
			Workers:         cfg.Workers,
		}
		return finishSettings(provider, settings)
	}

	return finishSettings(provider, cfg)
}

func finishSettings(provider *hosting.HostingProvider, cfg *settings) (*settings, error) {
	if cfg.Workers < 0 {
		return nil, errors.New("s3: workers must be at least 1")
	}
	if cfg.Region == "" {
		cfg.Region = defaultRegion
	}
	if cfg.AddressingStyle == "" {
		cfg.AddressingStyle = defaultAddressingStyle
	}
	switch cfg.AddressingStyle {
	case AddressingStylePath:
		cfg.UsePathStyle = true
	case AddressingStyleVirtual:
		cfg.UsePathStyle = false
	default:
		return nil, fmt.Errorf("s3: unknown addressing_style %q", cfg.AddressingStyle)
	}
	if cfg.AddressingStyle == AddressingStylePath && cfg.Bucket == "" {
		bucket, err := bucketFromEndpointPath(provider.Endpoint)
		if err != nil {
			return nil, err
		}
		cfg.Bucket = bucket
	}
	return cfg, nil
}

// Validate validates S3-specific configuration.
func (Engine) Validate(provider *hosting.HostingProvider) error {
	_, err := validateSettings(provider)
	return err
}

// Ready validates S3 configuration and confirms runtime credentials can build a
// local client. It intentionally does not perform remote S3 network probes.
func (engine Engine) Ready(ctx context.Context, secretProviders hosting.SecretsProviders, provider *hosting.HostingProvider) error {
	cfg, err := validateSettings(provider)
	if err != nil {
		return err
	}
	_, err = engine.newClient(ctx, secretProviders, provider, cfg)
	return err
}

func validateSettings(provider *hosting.HostingProvider) (*settings, error) {
	cfg, err := newSettings(provider)
	if err != nil {
		return nil, err
	}
	if cfg.AddressingStyle == AddressingStylePath && cfg.Bucket == "" {
		return nil, errors.New("s3: bucket is empty")
	}
	if cfg.ConditionalMode != ConditionalModeNone && cfg.ConditionalMode != ConditionalModeETag {
		return nil, fmt.Errorf("s3: unknown conditional_mode %q", cfg.ConditionalMode)
	}
	if err := provider.Secrets.Require(secretAccessKeyID, secretSecretAccessKey); err != nil {
		return nil, fmt.Errorf("s3: %w", err)
	}
	return cfg, nil
}

// Deploy discovers files under root, applies the provider filter, and uploads
// every selected file concurrently, bounded by the provider's Workers setting
// or the S3 default. Respects ctx.
func (engine Engine) deployFiles(ctx context.Context, secretProviders hosting.SecretsProviders, provider *hosting.HostingProvider, files []*hosting.File, kind hosting.DeploymentKind, w io.Writer) error {
	if err := kind.Validate(); err != nil {
		return err
	}
	cfg, err := newSettings(provider)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	client, err := engine.newClient(ctx, secretProviders, provider, cfg)
	if err != nil {
		return err
	}
	sem := make(chan struct{}, workersOrDefault(cfg.Workers))
	g, ctx := errgroup.WithContext(ctx)
	for _, file := range files {
		file := file
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		g.Go(func() error {
			defer func() { <-sem }()
			return engine.putOne(ctx, client, provider, cfg, file, w)
		})
	}
	return g.Wait()
}

func (Engine) ValidateOperation(provider *hosting.HostingProvider, operation hosting.DeploymentOperation) error {
	return nil
}

// PerformOperation preserves precondition, bulk, and final enqueue ordering.
func (engine Engine) PerformOperation(ctx context.Context, secretProviders hosting.SecretsProviders, operation hosting.DeploymentOperation, w io.Writer) error {
	for _, condition := range operation.Preconditions {
		if err := engine.compareAndSwap(ctx, secretProviders, operation.HostingProvider, condition, w); err != nil {
			return err
		}
	}
	if err := engine.deployFiles(ctx, secretProviders, operation.HostingProvider, operation.Bulk, operation.Kind, w); err != nil {
		return err
	}
	for _, file := range operation.Enqueued {
		if err := engine.deployFiles(ctx, secretProviders, operation.HostingProvider, []*hosting.File{file}, hosting.IncrementalDeployment, w); err != nil {
			return err
		}
	}
	return nil
}

// compareAndSwap provides an atomic conditional write at the S3 boundary.
// A replacement first proves exact remote bytes match Expected, then submits a
// PutObject bound to that object's authoritative ETag. A create uses
// If-None-Match: * and never falls back to a blind put.
func (engine Engine) compareAndSwap(ctx context.Context, secretProviders hosting.SecretsProviders, provider *hosting.HostingProvider, condition hosting.PreconditionFile, w io.Writer) error {
	cfg, err := newSettings(provider)
	if err != nil {
		return err
	}
	client, err := engine.newClient(ctx, secretProviders, provider, cfg)
	if err != nil {
		return err
	}
	input, err := conditionalPutInput(condition, cfg)
	if err != nil {
		return err
	}
	if condition.Expected == nil {
		input.IfNoneMatch = aws.String("*")
	} else {
		remote, err := client.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(condition.Target)})
		if err != nil {
			if isNotFoundHTTPError(err) {
				return fmt.Errorf("%w: target %q does not exist", hosting.ErrDeploymentPreconditionFailed, condition.Target)
			}
			return fmt.Errorf("s3: get precondition target %s: %w", condition.Target, err)
		}
		remoteBytes, readErr := io.ReadAll(remote.Body)
		closeErr := remote.Body.Close()
		if readErr != nil {
			return fmt.Errorf("s3: read precondition target %s: %w", condition.Target, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("s3: close precondition target %s: %w", condition.Target, closeErr)
		}
		if !bytes.Equal(remoteBytes, condition.Expected) || remote.ETag == nil {
			return fmt.Errorf("%w: target %q differs from expected contents", hosting.ErrDeploymentPreconditionFailed, condition.Target)
		}
		input.IfMatch = remote.ETag
	}
	if _, err := client.PutObject(ctx, input); err != nil {
		if isPreconditionError(err) {
			return fmt.Errorf("%w: target %q changed", hosting.ErrDeploymentPreconditionFailed, condition.Target)
		}
		return fmt.Errorf("s3: conditional put %s: %w", condition.Target, err)
	}
	return nil
}

func conditionalPutInput(condition hosting.PreconditionFile, cfg *settings) (*awss3.PutObjectInput, error) {
	body := bytes.NewReader(condition.Content)
	input := &awss3.PutObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(condition.Target), Body: body, ContentType: aws.String(hosting.MimetypeForPath(condition.Target))}
	if cfg.CacheControl != "" {
		input.CacheControl = aws.String(cfg.CacheControl)
	}
	return input, nil
}

func isPreconditionError(err error) bool {
	type statusCoder interface{ HTTPStatusCode() int }
	var status statusCoder
	return errors.As(err, &status) && (status.HTTPStatusCode() == 409 || status.HTTPStatusCode() == 412)
}

func workersOrDefault(workers int) int {
	if workers > 0 {
		return workers
	}
	return DefaultWorkers
}

func (engine Engine) putOne(ctx context.Context, client *awss3.Client, provider *hosting.HostingProvider, cfg *settings, file *hosting.File, w io.Writer) error {
	key := file.RelPath()
	destination := provider.DestinationURL(key)

	contentType, err := file.Mimetype()
	if err != nil {
		return fmt.Errorf("s3: mimetype %s: %w", key, err)
	}

	if cfg.ConditionalMode == ConditionalModeETag {
		skip, err := engine.skipByETag(ctx, client, cfg, key, file)
		if err != nil {
			return fmt.Errorf("s3: head %s: %w", key, err)
		}
		if skip {
			hosting.LogResource(w, ctx, provider, destination, contentType, "skipped")
			return nil
		}
	}

	hosting.LogResource(w, ctx, provider, destination, contentType, "put")

	body, err := file.Open()
	if err != nil {
		return fmt.Errorf("s3: open %s: %w", key, err)
	}
	defer body.Close()

	input := &awss3.PutObjectInput{
		Bucket:      aws.String(cfg.Bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	}
	if cfg.CacheControl != "" {
		input.CacheControl = aws.String(cfg.CacheControl)
	}

	if _, err := client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("s3: put %s: %w", key, err)
	}
	hosting.LogResource(w, ctx, provider, destination, contentType, "completed")
	return nil
}

// skipByETag HEADs the remote object and compares its ETag to the local MD5.
// Returns true when the upload may safely be skipped. A 404 (object not
// present) returns false with no error — the caller will PUT.
func (engine Engine) skipByETag(ctx context.Context, client *awss3.Client, cfg *settings, key string, file *hosting.File) (bool, error) {
	head, err := client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *s3types.NoSuchKey
		var nf *s3types.NotFound
		if errors.As(err, &nsk) || errors.As(err, &nf) {
			return false, nil
		}
		if isNotFoundHTTPError(err) {
			return false, nil
		}
		return false, err
	}
	if head.ETag == nil {
		return false, nil
	}
	local, err := file.ETag()
	if err != nil {
		return false, fmt.Errorf("local etag: %w", err)
	}
	return normalizeETag(*head.ETag) == local, nil
}

// isNotFoundHTTPError reports whether err is a 404 response wrapped by the
// AWS SDK's HTTP response error envelope.
func isNotFoundHTTPError(err error) bool {
	type statusCoder interface{ HTTPStatusCode() int }
	var sc statusCoder
	if errors.As(err, &sc) && sc.HTTPStatusCode() == 404 {
		return true
	}
	return false
}

// normalizeETag strips the surrounding quotes S3 returns in the ETag header.
func normalizeETag(etag string) string {
	return strings.Trim(etag, "\"")
}

// newClient builds the S3 client. Credentials are resolved from the provider's
// Secrets through secretProviders.
func (engine Engine) newClient(ctx context.Context, secretProviders hosting.SecretsProviders, provider *hosting.HostingProvider, settings *settings) (*awss3.Client, error) {
	access, err := provider.Secrets.Get(secretAccessKeyID)
	if err != nil {
		return nil, err
	}
	accessValue, err := secretProviders.Resolve(ctx, access.LookupKey())
	if err != nil {
		return nil, fmt.Errorf("s3: %s: %w", secretAccessKeyID, err)
	}
	secret, err := provider.Secrets.Get(secretSecretAccessKey)
	if err != nil {
		return nil, err
	}
	secretValue, err := secretProviders.Resolve(ctx, secret.LookupKey())
	if err != nil {
		return nil, fmt.Errorf("s3: %s: %w", secretSecretAccessKey, err)
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(settings.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessValue.String(), secretValue.String(), "")),
	)
	if err != nil {
		return nil, fmt.Errorf("s3: load config: %w", err)
	}

	return awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		applyClientOptions(o, provider.Endpoint, settings.UsePathStyle)
	}), nil
}

func applyClientOptions(o *awss3.Options, providerEndpoint string, usePathStyle bool) {
	endpoint, err := endpointWithoutBucketPath(providerEndpoint)
	if err != nil {
		endpoint = providerEndpoint
	}
	if endpoint != "" {
		o.BaseEndpoint = aws.String(endpoint)
	}
	o.UsePathStyle = usePathStyle
}

func bucketFromEndpointPath(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("s3: parse endpoint: %w", err)
	}
	bucket := strings.Trim(u.Path, "/")
	if bucket == "" {
		return "", nil
	}
	if strings.Contains(bucket, "/") {
		return "", fmt.Errorf("s3: endpoint path %q is not a bucket name", u.Path)
	}
	return bucket, nil
}

func endpointWithoutBucketPath(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("s3: parse endpoint: %w", err)
	}
	u.Path = ""
	u.RawPath = ""
	u.ForceQuery = false
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
