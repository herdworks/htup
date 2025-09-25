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
	types.DeployProviders["s3"] = func(config *types.DeployProviderConfig) types.DeployProvider { return &s3.S3DeployProvider{} }

	// register secret providers
	types.SecretProviders["env"] = func() types.SecretProvider { return &types.EnvSecretProvider{} }
}

// HyperProviders represents a collection of deploy providers
type HyperProviders struct {
	providers map[string]types.DeployProvider
}

// New creates HyperProviders from a providers configuration
func New(config *types.ProvidersConfig) (*HyperProviders, error) {
	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	providers := &HyperProviders{
		providers: make(map[string]types.DeployProvider),
	}

	// Create each provider
	for _, providerConfig := range config.Providers {
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

	providerConstructor, _ := types.DeployProviders[config.Kind]
	provider := providerConstructor(config)

	return provider, nil
}
