// Package s3 implements a hyperproviders.HostingProviderEngine backed by an
// S3-compatible object store (AWS S3, Cloudflare R2, MinIO, DigitalOcean
// Spaces, etc.).
package s3

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/herdworks/hyperproviders-go/hosting"
	"github.com/herdworks/hyperproviders-go/session"
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

func newSettings(provider *hosting.Provider) (*settings, error) {
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

func finishSettings(provider *hosting.Provider, cfg *settings) (*settings, error) {
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
func (Engine) Validate(provider *hosting.Provider) error {
	cfg, err := newSettings(provider)
	if err != nil {
		return err
	}
	if cfg.AddressingStyle == AddressingStylePath && cfg.Bucket == "" {
		return errors.New("s3: bucket is empty")
	}
	if cfg.ConditionalMode != ConditionalModeNone && cfg.ConditionalMode != ConditionalModeETag {
		return fmt.Errorf("s3: unknown conditional_mode %q", cfg.ConditionalMode)
	}
	if err := provider.Secrets.Require(secretAccessKeyID, secretSecretAccessKey); err != nil {
		return fmt.Errorf("s3: %w", err)
	}
	return nil
}

// Deploy discovers files under root, applies the provider filter, and uploads
// every selected file concurrently, bounded by the provider's Workers setting
// or the S3 default. Respects ctx.
func (engine Engine) Deploy(ctx context.Context, runtime *session.Session, provider *hosting.Provider, files []*hosting.File) error {
	cfg, err := newSettings(provider)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	client, err := engine.newClient(ctx, runtime, provider, cfg)
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
			return engine.putOne(ctx, client, cfg, file)
		})
	}
	return g.Wait()
}

func workersOrDefault(workers int) int {
	if workers > 0 {
		return workers
	}
	return DefaultWorkers
}

func (engine Engine) putOne(ctx context.Context, client *awss3.Client, cfg *settings, file *hosting.File) error {
	key := file.RelPath()

	if cfg.ConditionalMode == ConditionalModeETag {
		skip, err := engine.skipByETag(ctx, client, cfg, key, file)
		if err != nil {
			return fmt.Errorf("s3: head %s: %w", key, err)
		}
		if skip {
			return nil
		}
	}

	body, err := file.Open()
	if err != nil {
		return fmt.Errorf("s3: open %s: %w", key, err)
	}
	defer body.Close()

	contentType, err := file.Mimetype()
	if err != nil {
		return fmt.Errorf("s3: mimetype %s: %w", key, err)
	}

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
// Secrets through runtime.
func (engine Engine) newClient(ctx context.Context, runtime *session.Session, provider *hosting.Provider, settings *settings) (*awss3.Client, error) {
	access, err := provider.Secrets.Get(secretAccessKeyID)
	if err != nil {
		return nil, err
	}
	accessValue, err := runtime.ResolveSecret(ctx, access.LookupKey())
	if err != nil {
		return nil, fmt.Errorf("s3: %s: %w", secretAccessKeyID, err)
	}
	secret, err := provider.Secrets.Get(secretSecretAccessKey)
	if err != nil {
		return nil, err
	}
	secretValue, err := runtime.ResolveSecret(ctx, secret.LookupKey())
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
