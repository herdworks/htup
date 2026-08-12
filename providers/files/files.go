// Package files implements a htup.SecretsProvider backed by local files.
package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herdworks/htup"
)

const Kind = "files"

// Provider resolves secret keys as file paths and returns file contents.
type Provider struct {
	BaseDir string
}

// New returns a files secret provider registered under Kind.
func New() *Provider {
	return NewWithBaseDirectory("")
}

// NewWithBaseDirectory returns a files secret provider that resolves relative
// paths from baseDir. Absolute paths are always used as provided.
func NewWithBaseDirectory(baseDir string) *Provider {
	return &Provider{
		BaseDir: baseDir,
	}
}

func (*Provider) Name() string { return Kind }

// Resolve reads key as a filesystem path. Relative paths are joined to BaseDir
// when configured. Only ~/, $HOME/, and ${HOME}/ are expanded.
func (p *Provider) Resolve(ctx context.Context, key string) (htup.SecretValue, error) {
	path, err := p.resolvePath(key)
	if err != nil {
		return nil, err
	}
	value, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", htup.ErrSecretNotFound, key)
		}
		return nil, fmt.Errorf("read file secret %s: %w", key, err)
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("%w: %s", htup.ErrSecretEmpty, key)
	}
	return htup.BytesSecret(value), nil
}

func (p *Provider) resolvePath(key string) (string, error) {
	key, err := expandHome(key)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(key) || p == nil || p.BaseDir == "" {
		return key, nil
	}
	return filepath.Join(p.BaseDir, key), nil
}

func expandHome(key string) (string, error) {
	switch {
	case strings.HasPrefix(key, "~/"):
		return homePath(strings.TrimPrefix(key, "~/"))
	case strings.HasPrefix(key, "$HOME/"):
		return homePath(strings.TrimPrefix(key, "$HOME/"))
	case strings.HasPrefix(key, "${HOME}/"):
		return homePath(strings.TrimPrefix(key, "${HOME}/"))
	case strings.HasPrefix(key, "~"), strings.HasPrefix(key, "$"):
		return "", fmt.Errorf("unsupported file secret path expansion: %s", key)
	default:
		return key, nil
	}
}

func homePath(rel string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, rel), nil
}
