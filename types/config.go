package types

import (
	"fmt"
	"regexp"
)

// DeployProviderConfig represents configuration for a single deploy provider
type DeployProviderConfig struct {
	Name    string                 `json:"name" yaml:"name"`       // kebab/snake-case identifier (a-z, 0-9, -/_)
	Kind    string                 `json:"kind" yaml:"kind"`       // deploy provider type (e.g., "s3")
	Config  map[string]interface{} `json:"config" yaml:"config"`   // provider-specific configuration
	Secrets []SecretConfig         `json:"secrets" yaml:"secrets"` // provider secrets
}

// SecretConfig represents configuration for obtaining secrets
type SecretConfig struct {
	Name string `json:"name" yaml:"name"` // secret name used by deploy provider
	Kind string `json:"kind" yaml:"kind"` // secret provider type (e.g., "env")
	Key  string `json:"key" yaml:"key"`   // key/path to the secret value
}

// validateName validates that a provider name follows kebab-case rules
func (c *DeployProviderConfig) validateName() error {
	if c.Name == "" {
		return fmt.Errorf("provider name is required")
	}

	// Validate kebab-case or snake-case: a-z, 0-9, and '-'/'_' only
	kebabRegex := regexp.MustCompile(`^[a-z0-9-_]+$`)
	if !kebabRegex.MatchString(c.Name) {
		return fmt.Errorf("provider name '%s' must be kebab-case (a-z, 0-9, and '-'/'_' only)", c.Name)
	}

	return nil
}

// validateKind validates that the provider kind is supported
func (c *DeployProviderConfig) validateKind() error {
	if c.Kind == "" {
		return fmt.Errorf("provider kind is required")
	}

	// Check if the deploy provider kind is supported
	_, ok := DeployProviders[c.Kind]
	if ok {
		return nil // Kind is valid
	}

	return fmt.Errorf("unsupported provider kind '%s'", c.Kind)
}

// validateSecrets validates the secrets configuration
func (c *DeployProviderConfig) validateSecrets() error {
	for i, secret := range c.Secrets {
		if secret.Name == "" {
			return fmt.Errorf("secret[%d] name is required", i)
		}
		if secret.Kind == "" {
			return fmt.Errorf("secret[%d] kind is required", i)
		}
		if secret.Key == "" {
			return fmt.Errorf("secret[%d] key is required", i)
		}

		// Check if the deploy provider kind is supported
		_, ok := SecretProviders[secret.Kind]
		if !ok {
			return fmt.Errorf("unsupported secret kind '%s'", secret.Kind)
		}
	}
	return nil
}

// Validate validates the entire provider configuration
func (c *DeployProviderConfig) Validate() error {
	if err := c.validateName(); err != nil {
		return err
	}
	if err := c.validateKind(); err != nil {
		return err
	}
	if err := c.validateSecrets(); err != nil {
		return err
	}
	return nil
}
