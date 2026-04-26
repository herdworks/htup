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
	Version      int                        `json:"version"      yaml:"version"`
	Environments map[string]*hp.Environment `json:"environments" yaml:"environments"`
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
	if cfg.Environments == nil {
		return nil, fmt.Errorf("config %s: environments is empty", path)
	}
	for name, env := range cfg.Environments {
		if env.Name == "" {
			env.Name = name
		}
	}
	return &cfg, nil
}

// Environment returns the environment with the given name.
func (c *Config) Environment(name string) (*hp.Environment, error) {
	env, ok := c.Environments[name]
	if !ok {
		return nil, fmt.Errorf("environment %q not found in config", name)
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
