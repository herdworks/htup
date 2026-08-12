// Package env implements a htup.SecretsProvider backed by OS environment variables.
package env

import (
	"context"
	"fmt"
	"os"

	"github.com/herdworks/htup"
)

const Kind = "env"

// Provider resolves secrets from OS environment variables.
type Provider struct{}

// New returns an env secret provider registered under Kind.
func New() *Provider { return &Provider{} }

func (*Provider) Name() string { return Kind }

// Resolve returns the environment variable value for key.
func (p *Provider) Resolve(ctx context.Context, key string) (htup.SecretValue, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil, fmt.Errorf("%w: %s", htup.ErrSecretNotFound, key)
	}
	if value == "" {
		return nil, fmt.Errorf("%w: %s", htup.ErrSecretEmpty, key)
	}
	return htup.StringSecret(value), nil
}
