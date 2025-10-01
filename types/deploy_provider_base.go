package types

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// BaseDeployProvider provides common functionality for all deploy providers
type BaseDeployProvider struct {
	config          *DeployProviderConfig
	secretProviders map[string]SecretProvider
}

// Factory function for BaseDeployProvider
func NewBaseDeployProvider(config *DeployProviderConfig) (*BaseDeployProvider, error) {
	secretProviders := make(map[string]SecretProvider)
	for _, secret := range config.Secrets {
		if _, exists := secretProviders[secret.Kind]; !exists {
			// Build the secret provider
			secretProviderConstructor := SecretProviders[secret.Kind]
			secretProvider := secretProviderConstructor()
			secretProviders[secret.Kind] = secretProvider
		}
	}
	return &BaseDeployProvider{config: config, secretProviders: secretProviders}, nil
}

// GetConfig returns a configuration value as a string
func (b *BaseDeployProvider) GetConfig(key string) (string, error) {
	if b.config == nil {
		return "", fmt.Errorf("provider configuration is not set")
	}
	if val, ok := b.config.Config[key]; ok {
		if str, ok := val.(string); ok {
			return str, nil
		}
		return fmt.Sprintf("%v", val), nil
	}
	return "", fmt.Errorf("configuration key '%s' not found", key)
}

// GetConfigWithDefault returns a configuration value as a string with a default fallback
func (b *BaseDeployProvider) GetConfigWithDefault(key, defaultVal string) string {
	if val, err := b.GetConfig(key); err == nil {
		return val
	}
	return defaultVal
}

// GetSecret returns a secret value as a string
func (b *BaseDeployProvider) GetSecret(key string) (string, error) {
	for _, secret := range b.config.Secrets {
		if secret.Name == key {
			if val, err := b.secretProviders[secret.Kind].GetSecret(secret.Key); err == nil {
				return val, err
			}
			return "", fmt.Errorf("secret key '%s' was not found by the '%s' secrets provider", key, secret.Kind)
		}
	}
	return "", fmt.Errorf("secret key '%s' was not found in the configuration", key)
}

// DetectMimeType detects MIME type for a file path
func (b *BaseDeployProvider) DetectMimeType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	// Get MIME type from extension
	mimeType := mime.TypeByExtension(ext)
	if mimeType != "" {
		return mimeType
	}

	// Fallback: try to detect from file content
	file, err := os.Open(filePath)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()

	// Read first 512 bytes to detect content type
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return "application/octet-stream"
	}

	contentType := http.DetectContentType(buffer[:n])
	return contentType
}

// WalkFiles walks through files in a directory and calls the provided function
func (b *BaseDeployProvider) WalkFiles(buildPath string, fn func(filePath, relativePath string) error) error {
	return filepath.Walk(buildPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Calculate relative path
		relativePath, err := filepath.Rel(buildPath, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}

		// Convert to forward slashes for web paths
		relativePath = strings.ReplaceAll(relativePath, "\\", "/")

		return fn(path, relativePath)
	})
}
