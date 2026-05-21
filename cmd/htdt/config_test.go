package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_JSON(t *testing.T) {
	path := writeConfig(t, "config.json", `{
		"version": 1,
		"providers": {
			"assets": {
				"kind": "s3",
				"endpoint": "https://example.com",
				"base_url": "https://assets.example.com",
				"config": { "bucket": "assets" }
			}
		},
		"environments": {
			"production": ["assets"]
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
providers:
  assets:
    kind: s3
    endpoint: https://example.com
    base_url: https://assets.example.com
    config:
      bucket: assets
environments:
  production:
    - assets
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
providers:
  assets:
    kind: s3
    endpoint: https://example.com
    base_url: https://assets.example.com
    config:
      bucket: assets
environments:
  production:
    - assets
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	assertLoadedConfig(t, cfg)
}

func TestRunReady_UsesEnvironmentSecrets(t *testing.T) {
	path := writeReadyConfig(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")

	if err := runReady(context.Background(), []string{"--config", path, "--environment", "production"}); err != nil {
		t.Fatalf("runReady: %v", err)
	}
}

func TestRunReady_ReportsMissingSecret(t *testing.T) {
	path := writeReadyConfig(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	err := runReady(context.Background(), []string{"--config", path, "--environment", "production"})
	if err == nil {
		t.Fatal("runReady error = nil, want missing secret")
	}
	if !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
		t.Fatalf("runReady error = %v, want missing access key", err)
	}
}

func writeConfig(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeReadyConfig(t *testing.T) string {
	t.Helper()
	return writeConfig(t, "ready.json", `{
		"version": 1,
		"providers": {
			"assets": {
				"kind": "s3",
				"endpoint": "https://example.com",
				"config": { "bucket": "assets" },
				"secrets": [
					{ "name": "access_key_id", "key": "AWS_ACCESS_KEY_ID" },
					{ "name": "secret_access_key", "key": "AWS_SECRET_ACCESS_KEY" }
				]
			}
		},
		"environments": {
			"production": ["assets"]
		}
	}`)
}

func assertLoadedConfig(t *testing.T, cfg *Config) {
	t.Helper()
	env, err := cfg.Environment("production")
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if len(*env) != 1 || (*env)[0] != "assets" {
		t.Fatalf("environment = %#v, want [assets]", *env)
	}
	provider := cfg.Providers["assets"]
	if provider == nil {
		t.Fatal("provider assets missing")
	}
	if provider.Kind != "s3" {
		t.Errorf("provider.Kind = %q, want s3", provider.Kind)
	}
	if provider.BaseURL != "https://assets.example.com" {
		t.Errorf("provider.BaseURL = %q, want assets base URL", provider.BaseURL)
	}
}
