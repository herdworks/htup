package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	hp "github.com/herdworks/hyperproviders-go"
	"github.com/herdworks/hyperproviders-go/providers/env"
	_ "github.com/herdworks/hyperproviders-go/providers/git"
	_ "github.com/herdworks/hyperproviders-go/providers/s3"
	"gopkg.in/yaml.v3"
)

// Config is the top-level htdt configuration.
type Config struct {
	Version      int                               `json:"version"      yaml:"version"`
	Providers    hp.HostingProviders               `json:"providers"    yaml:"providers"`
	Environments map[string]*hp.HostingEnvironment `json:"environments" yaml:"environments"`
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
func (c *Config) Environment(name string) (*hp.HostingEnvironment, error) {
	env, ok := c.Environments[name]
	if !ok {
		return nil, fmt.Errorf("environment %q not found in config", name)
	}
	if env == nil {
		return nil, fmt.Errorf("environment %q is empty in config", name)
	}
	return env, nil
}

// Session returns the default htdt session. The CLI resolves secrets from OS
// environment variables because config files are durable project state.
func Session() *hp.Session {
	return &hp.Session{
		SecretProviders: []hp.SecretProvider{env.New()},
	}
}
