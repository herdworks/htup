package s3

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/herdworks/hyperproviders-go/types"
)

// DeployProvider implements the DeployProvider interface for S3-compatible storage
type S3DeployProvider struct {
	types.BaseDeployProvider
	endpoint  string
	bucket    string
	accessKey string
	secretKey string
}

// S3DeployProvider Constructor
func NewS3Provider(config *types.DeployProviderConfig) *S3DeployProvider {
	// Call the "constructor" for the embedded struct
	baseProvider := types.NewBaseDeployProvider(config)

	s3Provider := &S3DeployProvider{
		BaseDeployProvider: *baseProvider,
	}

	s3Provider.Validate()

	return s3Provider
}

// Deploy uploads files from buildPath to S3 using the given base URL
func (p *S3DeployProvider) Deploy(ctx context.Context, buildPath, baseURL string) error {
	// Parse base URL to extract domain
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL '%s': %w", baseURL, err)
	}
	domain := parsedURL.Host

	// Initialize S3 client and uploader
	client, err := p.newS3Client(ctx)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}
	uploader := manager.NewUploader(client)

	// Use errgroup for concurrent uploads
	taskgroup, groupCtx := errgroup.WithContext(ctx)

	// Walk through all files and upload them
	err = p.WalkFiles(buildPath, func(filePath, relativePath string) error {
		taskgroup.Go(func() error {
			return p.uploadFile(groupCtx, uploader, filePath, relativePath, domain)
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk files: %w", err)
	}

	// Wait for all uploads to complete
	if err := taskgroup.Wait(); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	return nil
}

// Validates S3-specific configuration and required secrets
func (p *S3DeployProvider) Validate() error {
	// Extract and validate S3-specific configuration
	var err error
	if p.bucket, err = p.GetConfig("store"); err != nil {
		return fmt.Errorf("store (bucket name) is required: %w", err)
	}
	if p.endpoint, err = p.GetConfig("endpoint"); err != nil {
		return fmt.Errorf("endpoint is required: %w", err)
	}

	// Validate endpoint format
	if p.endpoint != "" {
		if _, err := url.Parse(p.endpoint); err != nil {
			return fmt.Errorf("invalid endpoint URL: %w", err)
		}
	}

	// Extract secrets
	if p.accessKey, err = p.GetSecret("access_key_id"); err != nil {
		return fmt.Errorf("access_key_id secret is required: %w", err)
	}
	if p.secretKey, err = p.GetSecret("secret_access_key"); err != nil {
		return fmt.Errorf("secret_access_key secret is required: %w", err)
	}

	return nil
}

// uploadFile uploads a single file to S3
func (p *S3DeployProvider) uploadFile(ctx context.Context, uploader *manager.Uploader, filePath, relativePath, domain string) error {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	// Determine content type
	contentType := p.DetectMimeType(filePath)

	// Create the upload input
	input := &s3.PutObjectInput{
		Bucket:      aws.String(p.bucket),
		Key:         aws.String(relativePath),
		Body:        file,
		ContentType: aws.String(contentType),
		Metadata: map[string]string{
			"uploaded-by": "hyperproviders-go",
		},
	}

	// Set cache control based on file type
	cacheControl := p.getCacheControl(relativePath)
	if cacheControl != "" {
		input.CacheControl = aws.String(cacheControl)
	}

	// Upload the file
	fmt.Printf("Uploading %s to s3://%s/%s\n", relativePath, p.bucket, relativePath)
	_, err = uploader.Upload(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to upload %s: %w", relativePath, err)
	}

	// Construct the URL for logging
	baseURL := fmt.Sprintf("https://%s", domain)
	if !strings.HasPrefix(relativePath, "/") {
		relativePath = "/" + relativePath
	}
	fullURL := baseURL + relativePath

	fmt.Printf("Published %s (%s)\n", fullURL, contentType)
	return nil
}

// getCacheControl returns appropriate cache control headers based on file type
func (p *S3DeployProvider) getCacheControl(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	filename := strings.ToLower(filepath.Base(filePath))

	switch ext {
	case ".html", ".htm":
		return "no-cache, no-store, must-revalidate"
	case ".css", ".js", ".mjs":
		return "public, max-age=31536000" // 1 year
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
		return "public, max-age=2592000" // 30 days
	case ".woff", ".woff2", ".ttf", ".otf", ".eot":
		return "public, max-age=31536000" // 1 year
	case ".xml":
		if strings.Contains(filename, "sitemap") {
			return "public, max-age=3600" // 1 hour
		}
		return "public, max-age=1800" // 30 minutes
	case ".txt":
		if filename == "robots.txt" {
			return "public, max-age=86400" // 1 day
		}
		return "public, max-age=3600" // 1 hour
	case ".ico":
		if filename == "favicon.ico" {
			return "public, max-age=86400" // 1 day
		}
		return "public, max-age=2592000" // 30 days
	default:
		return "public, max-age=3600" // 1 hour
	}
}

// newS3Client creates a new S3 client with the provider's configuration
func (p *S3DeployProvider) newS3Client(ctx context.Context) (*s3.Client, error) {

	// Load AWS configuration with static credentials
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			p.accessKey,
			p.secretKey,
			"", // session token
		)),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client
	if p.endpoint == "" || p.endpoint == "https://s3.amazonaws.com" {
		// Standard AWS S3
		return s3.NewFromConfig(cfg), nil
	} else {
		// S3-compatible service with custom endpoint
		return s3.NewFromConfig(cfg, func(options *s3.Options) {
			options.BaseEndpoint = aws.String(p.endpoint)
			options.UsePathStyle = true
		}), nil
	}
}
