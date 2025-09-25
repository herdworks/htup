package types

import "context"

// DeployProvider defines the interface that all deploy providers must implement
type DeployProvider interface {
	// Deploy uploads files from buildPath to the provider using the given base URL
	Deploy(ctx context.Context, buildPath string, baseURL string) error

	// Validate validates provider-specific configuration
	Validate() error
}

// SecretProvider defines the interface that all secret providers must implement
type SecretProvider interface {
	// GetSecret retrieves a secret value by key
	GetSecret(key string) (string, error)

	// Validate validates provider-specific configuration
	Validate(key string) error
}

// Factory function types for creating providers
type NewDeployProviderFunc func(config *DeployProviderConfig) DeployProvider
type NewSecretProviderFunc func() SecretProvider

// Global provider registries
var DeployProviders = make(map[string]NewDeployProviderFunc)
var SecretProviders = make(map[string]NewSecretProviderFunc)
