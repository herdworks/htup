package s3

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	endpoint    string
	bucket      string
	accessKey   string
	secretKey   string
	smartUpload bool
}

// S3DeployProvider Constructor
func NewS3Provider(config *types.DeployProviderConfig) (*S3DeployProvider, error) {
	// Call the "constructor" for the embedded struct
	baseProvider, err := types.NewBaseDeployProvider(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create base deploy provider: %w", err)
	}

	s3Provider := &S3DeployProvider{
		BaseDeployProvider: *baseProvider,
	}

	err = s3Provider.Validate()
	if err != nil {
		return nil, fmt.Errorf("Warning: S3 provider validation error: %v\n", err)
	}

	slog.Debug("S3 provider created", "name", config.Name)
	slog.Debug("S3 provider config:", "endpoint", s3Provider.endpoint, "bucket", s3Provider.bucket, "smart_upload", s3Provider.smartUpload)

	return s3Provider, nil
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
			return p.uploadFile(groupCtx, client, uploader, filePath, relativePath, domain)
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

	// Get smart upload setting (optional, defaults to false)
	if smartUploadStr, err := p.GetConfig("smart_upload"); err == nil {
		if parsed, parseErr := strconv.ParseBool(smartUploadStr); parseErr == nil {
			p.smartUpload = parsed
		}
	}

	return nil
}

// uploadFile uploads a single file to S3
func (p *S3DeployProvider) uploadFile(ctx context.Context, s3Client *s3.Client, uploader *manager.Uploader, filePath, relativePath, domain string) error {
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
	slog.Info("Uploading:", "file", relativePath, "destination", fmt.Sprintf("s3://%s/%s\n", p.bucket, relativePath))

	if p.smartUpload {
		// Use conditional upsert logic
		err = p.conditionalUploadWithRetry(ctx, s3Client, uploader, input, filePath, relativePath)
	} else {
		// Use simple upload logic
		_, err = uploader.Upload(ctx, input)
	}

	if err != nil {
		return fmt.Errorf("failed to upload %s: %w", relativePath, err)
	}

	// Construct the URL for logging
	baseURL := fmt.Sprintf("https://%s", domain)
	if !strings.HasPrefix(relativePath, "/") {
		relativePath = "/" + relativePath
	}
	fullURL := baseURL + relativePath

	slog.Debug(fmt.Sprintf("Published %s (%s)", fullURL, contentType))
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

// calculateFileMD5 calculates the MD5 hash of a local file
func (p *S3DeployProvider) calculateFileMD5(filePath string) (string, error) {
	slog.Debug("Calculating MD5:", "file", filePath)
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to calculate MD5 hash: %w", err)
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// getFileSize returns the size of a local file
func (p *S3DeployProvider) getFileSize(filePath string) (int64, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to get file stat: %w", err)
	}
	return stat.Size(), nil
}

// conditionalUploadWithRetry implements the conditional upsert logic with retry mechanism
func (p *S3DeployProvider) conditionalUploadWithRetry(ctx context.Context, s3Client *s3.Client, uploader *manager.Uploader, input *s3.PutObjectInput, filePath, relativePath string) error {
	const maxRetries = 5

	var localSize *int64 // local file size (pointer to handle the nil case)
	var localMD5 string  // local MD5 hash

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms, 800ms, 1600ms
			backoffDuration := time.Duration(100<<attempt) * time.Millisecond
			slog.Debug("Retrying upload:", "file", relativePath, "attempt", attempt+1, "max_attempts", maxRetries, "backoff", backoffDuration)
			time.Sleep(backoffDuration)
		}

		// Step 1: Try to upload with If-None-Match: "*"
		inputCopy := *input
		inputCopy.IfNoneMatch = aws.String("*")

		_, err := uploader.Upload(ctx, &inputCopy)
		if err == nil {
			// Upload succeeded - object didn't exist before
			slog.Debug("Uploaded new object:", "file", relativePath)
			return nil
		}

		// Check if it's a 412 error (object already exists)
		if !strings.Contains(err.Error(), "412") && !strings.Contains(err.Error(), "PreconditionFailed") {
			// Some other error occurred
			return fmt.Errorf("failed to upload %s: %w", relativePath, err)
		}

		// Step 2: Object exists, get its metadata
		headResp, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: input.Bucket,
			Key:    input.Key,
		})
		if err != nil {
			return fmt.Errorf("failed to get object metadata for %s: %w", relativePath, err)
		}

		remoteSize := headResp.ContentLength
		remoteMD5 := headResp.Metadata["ht-md5"]
		remoteETag := aws.ToString(headResp.ETag)
		// Remove quotes from ETag if present
		if len(remoteETag) > 2 && remoteETag[0] == '"' && remoteETag[len(remoteETag)-1] == '"' {
			remoteETag = remoteETag[1 : len(remoteETag)-1]
		}

		// Step 3: Compare file sizes
		if localSize == nil {
			// Get local file size only once if we're in a retry loop
			size, err := p.getFileSize(filePath)
			if err != nil {
				return fmt.Errorf("failed to get local file size: %w", err)
			}
			localSize = &size
		}

		if *localSize == *remoteSize {
			// Step 4: Sizes match, compare MD5 hashes
			if localMD5 == "" {
				// Get local MD5 sum only once if we're in a retry loop
				localMD5, err = p.calculateFileMD5(filePath)
				if err != nil {
					return fmt.Errorf("failed to calculate local file MD5: %w", err)
				}
			}

			if remoteMD5 != "" {
				// We have a stored MD5, use it
				if localMD5 == remoteMD5 {
					// Files are identical, no need to upload
					slog.Debug("Skipped upload of identical file (MD5):", "file", relativePath)
					return nil
				}
			} else {
				// No stored MD5, fall back to ETag comparison
				if localMD5 == remoteETag {
					// Files are identical, no need to upload
					slog.Debug("Skipped upload of identical file (ETag):", "file", relativePath)
					return nil
				}
			}
			// This code is commented out because it is not strictly necessary, however we may some day want it,
			// so leaving it here for now. The MD5 vs ETag comparison should be sufficient to determine if the
			// files are identical or not, in most cases. If they are not identical, we will proceed to upload
			// with If-Match condition. However, if you want to be absolutely sure, you can uncomment this code
			// to fetch the remote object and calculate its MD5 hash for a definitive comparison.
			/*

				// Step 5: MD5s don't match, fetch remote object to get actual content
				getResp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
					Bucket: input.Bucket,
					Key:    input.Key,
				})
				if err != nil {
					return fmt.Errorf("failed to get object content for %s: %w", relativePath, err)
				}
				defer getResp.Body.Close()

				// Update ETag from GET response, just to minimize race conditions
				if getResp.ETag != nil {
					remoteETag = aws.ToString(getResp.ETag)
					if len(remoteETag) > 2 && remoteETag[0] == '"' && remoteETag[len(remoteETag)-1] == '"' {
						remoteETag = remoteETag[1 : len(remoteETag)-1]
					}
				}

				// Step 6: Calculate MD5 of remote content
				remoteMD5Hash := md5.New()
				if _, err := io.Copy(remoteMD5Hash, getResp.Body); err != nil {
					return fmt.Errorf("failed to calculate remote file MD5: %w", err)
				}
				remoteMD5 := fmt.Sprintf("%x", remoteMD5Hash.Sum(nil))

				if localMD5 == remoteMD5 {
					// Files have identical content, no need to upload
					return nil
				}

			*/
		}

		// Step 7: Upload with If-Match condition
		inputCopy = *input
		inputCopy.IfMatch = aws.String(remoteETag)
		inputCopy.Metadata = map[string]string{
			"ht-md5": localMD5,
		}

		_, err = uploader.Upload(ctx, &inputCopy)
		if err == nil {
			// Upload succeeded
			slog.Debug("Uploaded updated object:", "file", relativePath)
			return nil
		}

		// Check if it's a 412 error (object changed since we read it)
		if !strings.Contains(err.Error(), "412") && !strings.Contains(err.Error(), "PreconditionFailed") {
			// Some other error occurred
			return fmt.Errorf("failed to upload %s: %w", relativePath, err)
		}

		// Object changed since we read it, retry the whole process
		continue
	}

	return fmt.Errorf("failed to upload %s after %d attempts due to concurrent modifications", relativePath, maxRetries)
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
