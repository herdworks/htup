package hyperproviders

import (
	"context"
	"fmt"

	"github.com/herdworks/hyperproviders-go/s3"
	"github.com/herdworks/hyperproviders-go/types"
)

// init() registers the types and their constructors.
func init() {
	// register deploy providers
	types.DeployProviders["s3"] = func(config *types.DeployProviderConfig) (types.DeployProvider, error) {
		return s3.NewS3Provider(config)
	}

	// register secret providers
	types.SecretProviders["env"] = func() types.SecretProvider { return &types.EnvSecretProvider{} }
}

// HyperProviders represents a collection of deploy providers
type HyperProviders struct {
	providers map[string]types.DeployProvider
}

// New creates HyperProviders from a providers configuration
func New(configs []types.DeployProviderConfig) (*HyperProviders, error) {
	// Validate the configuration
	if len(configs) == 0 {
		return nil, fmt.Errorf("at least one provider must be configured")
	}

	// Check for duplicate names
	names := make(map[string]bool)
	for _, provider := range configs {
		if names[provider.Name] {
			return nil, fmt.Errorf("duplicate provider name '%s'", provider.Name)
		}
		names[provider.Name] = true
	}

	providers := &HyperProviders{
		providers: make(map[string]types.DeployProvider),
	}

	// Create each provider
	for _, providerConfig := range configs {
		deployProvider, err := createProvider(&providerConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider '%s': %w", providerConfig.Name, err)
		}
		providers.providers[providerConfig.Name] = deployProvider
	}

	return providers, nil
}

// Deploy deploys to all configured providers
func (hps *HyperProviders) Deploy(ctx context.Context, buildPath, baseURL string) error {
	for name, provider := range hps.providers {
		provider.Validate()
		if err := provider.Deploy(ctx, buildPath, baseURL); err != nil {
			return fmt.Errorf("deployment to provider '%s' failed: %w", name, err)
		}
	}
	return nil
}

// createProvider creates a provider instance from its configuration
func createProvider(config *types.DeployProviderConfig) (types.DeployProvider, error) {
	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	providerConstructor := types.DeployProviders[config.Kind]
	provider, err := providerConstructor(config)
	if err != nil {
		return nil, fmt.Errorf("provider construction failed: %w", err)
	}

	return provider, nil
}
