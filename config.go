package hyperproviders

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DeploymentConfig is the provider selection projection of a repository-root
// project configuration. Other top-level configuration is intentionally ignored.
type DeploymentConfig struct {
	Providers    HostingProviders        `json:"providers" yaml:"providers"`
	Environments map[string]*Environment `json:"environments" yaml:"environments"`
}

// ProvidersForEnvironment resolves one named environment against this config.
func (config DeploymentConfig) ProvidersForEnvironment(name string) (HostingProviders, error) {
	if name == "" {
		return nil, fmt.Errorf("deployment environment is required")
	}
	environment := config.Environments[name]
	if environment == nil {
		return nil, fmt.Errorf("deployment environment %q not found", name)
	}
	return environment.Select(config.Providers)
}

// ValidateDeploymentConfigPath verifies that filename names a regular
// non-symlink configuration file.
func ValidateDeploymentConfigPath(filename string) error {
	if filename == "" {
		return fmt.Errorf("deployment config path is required")
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("deployment config must be a regular file: %s", filename)
	}
	return nil
}

// ReadDeploymentConfig reads a regular YAML or JSON configuration file without
// interpreting its contents.
func ReadDeploymentConfig(filename string) ([]byte, error) {
	if err := ValidateDeploymentConfigPath(filename); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// ParseDeploymentConfig decodes the provider selection projection from raw
// configuration bytes read from filename.
func ParseDeploymentConfig(filename string, raw []byte) (DeploymentConfig, error) {
	if filename == "" {
		return DeploymentConfig{}, fmt.Errorf("deployment config path is required")
	}
	var config DeploymentConfig
	var err error
	if strings.EqualFold(filepath.Ext(filename), ".yaml") || strings.EqualFold(filepath.Ext(filename), ".yml") {
		err = yaml.Unmarshal(raw, &config)
	} else {
		err = json.Unmarshal(raw, &config)
	}
	if err != nil {
		return DeploymentConfig{}, fmt.Errorf("parse deployment config: %w", err)
	}
	return config, nil
}

// LoadDeploymentConfig reads and decodes a regular YAML or JSON configuration file.
func LoadDeploymentConfig(filename string) (DeploymentConfig, error) {
	raw, err := ReadDeploymentConfig(filename)
	if err != nil {
		return DeploymentConfig{}, err
	}
	return ParseDeploymentConfig(filename, raw)
}
