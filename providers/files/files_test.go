package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hosting "github.com/herdworks/hyperproviders-go"
)

func TestProvider_ResolveAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy_key.pem")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := New()
	value, err := provider.Resolve(context.Background(), path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "secret" {
		t.Errorf("Resolve = %q, want secret", value.String())
	}
}

func TestProvider_ResolveRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("deploy_key.pem", []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := New()
	value, err := provider.Resolve(context.Background(), "deploy_key.pem")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "secret" {
		t.Errorf("Resolve = %q, want secret", value.String())
	}
}

func TestProvider_ResolveRelativePathFromBaseDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deploy_key.pem"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := NewWithBaseDirectory(dir)
	value, err := provider.Resolve(context.Background(), "deploy_key.pem")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "secret" {
		t.Errorf("Resolve = %q, want secret", value.String())
	}
}

func TestProvider_ResolveTildeHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "deploy_key.pem"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := NewWithBaseDirectory(t.TempDir())
	value, err := provider.Resolve(context.Background(), "~/deploy_key.pem")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "secret" {
		t.Errorf("Resolve = %q, want secret", value.String())
	}
}

func TestProvider_ResolveDollarHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "deploy_key.pem"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := NewWithBaseDirectory(t.TempDir())
	value, err := provider.Resolve(context.Background(), "$HOME/deploy_key.pem")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "secret" {
		t.Errorf("Resolve = %q, want secret", value.String())
	}
}

func TestProvider_ResolveBracedHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "deploy_key.pem"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := NewWithBaseDirectory(t.TempDir())
	value, err := provider.Resolve(context.Background(), "${HOME}/deploy_key.pem")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "secret" {
		t.Errorf("Resolve = %q, want secret", value.String())
	}
}

func TestProvider_ResolveRejectsUnsupportedExpansion(t *testing.T) {
	provider := New()
	for _, key := range []string{"~caleb/deploy_key.pem", "$DEPLOY_KEY"} {
		t.Run(key, func(t *testing.T) {
			_, err := provider.Resolve(context.Background(), key)
			if err == nil {
				t.Fatal("Resolve error = nil, want unsupported expansion")
			}
			if !strings.Contains(err.Error(), "unsupported file secret path expansion") {
				t.Fatalf("Resolve err = %v, want unsupported expansion", err)
			}
		})
	}
}

func TestProvider_ResolveMissing(t *testing.T) {
	provider := New()
	_, err := provider.Resolve(context.Background(), filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, hosting.ErrSecretNotFound) {
		t.Fatalf("Resolve err = %v, want ErrSecretNotFound", err)
	}
}

func TestProvider_ResolveEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	provider := New()
	_, err := provider.Resolve(context.Background(), path)
	if !errors.Is(err, hosting.ErrSecretEmpty) {
		t.Fatalf("Resolve err = %v, want ErrSecretEmpty", err)
	}
}
