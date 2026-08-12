package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herdworks/htup"
	"github.com/herdworks/htup/providers/env"
	_ "github.com/herdworks/htup/providers/git"
	_ "github.com/herdworks/htup/providers/s3"
	"gopkg.in/yaml.v3"
)

// Config is the top-level htup configuration.
type Config struct {
	Version      int                          `json:"version"      yaml:"version"`
	Providers    htup.HostingProviders        `json:"providers"    yaml:"providers"`
	Environments map[string]*htup.Environment `json:"environments" yaml:"environments"`
}

// LoadConfig reads and parses a JSON or YAML config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	default:
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("config %s: providers is empty", path)
	}
	if len(cfg.Environments) == 0 {
		return nil, fmt.Errorf("config %s: environments is empty", path)
	}
	return &cfg, nil
}

// Environment returns the environment with the given name.
func (c *Config) Environment(name string) (*htup.Environment, error) {
	env, ok := c.Environments[name]
	if !ok {
		return nil, fmt.Errorf("environment %q not found in config", name)
	}
	if env == nil {
		return nil, fmt.Errorf("environment %q is empty in config", name)
	}
	return env, nil
}

// SecretProviders returns the default htup secret providers. The CLI resolves
// secrets from OS environment variables because config files are durable project state.
func SecretProviders() htup.SecretsProviders {
	return htup.SecretsProviders{env.New()}
}
