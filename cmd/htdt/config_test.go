package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_JSON(t *testing.T) {
	path := writeConfig(t, "config.json", `{
		"version": 1,
		"environments": {
			"production": {
				"providers": [
					{
						"kind": "s3",
						"name": "assets",
						"endpoint": "https://example.com",
						"config": { "bucket": "assets" }
					}
				]
			}
		}
	}`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	assertLoadedConfig(t, cfg)
}

func TestLoadConfig_YAML(t *testing.T) {
	path := writeConfig(t, "config.yaml", `
version: 1
environments:
  production:
    providers:
      - kind: s3
        name: assets
        endpoint: https://example.com
        config:
          bucket: assets
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	assertLoadedConfig(t, cfg)
}

func TestLoadConfig_YML(t *testing.T) {
	path := writeConfig(t, "config.yml", `
version: 1
environments:
  production:
    providers:
      - kind: s3
        name: assets
        endpoint: https://example.com
        config:
          bucket: assets
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	assertLoadedConfig(t, cfg)
}

func writeConfig(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertLoadedConfig(t *testing.T, cfg *Config) {
	t.Helper()
	env, err := cfg.Environment("production")
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if env.Name != "production" {
		t.Errorf("env.Name = %q, want production", env.Name)
	}
	if len(env.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(env.Providers))
	}
	provider := env.Providers[0]
	if provider.Kind != "s3" {
		t.Errorf("provider.Kind = %q, want s3", provider.Kind)
	}
	if provider.DisplayName() != "assets" {
		t.Errorf("provider.DisplayName = %q, want assets", provider.DisplayName())
	}
}
