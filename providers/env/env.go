// Package env implements a hosting.SecretsProvider backed by OS environment variables.
package env

import (
	"context"
	"fmt"
	"os"

	hosting "github.com/herdworks/hyperproviders-go"
)

const Kind = "env"

// Provider resolves secrets from OS environment variables.
type Provider struct{}

// New returns an env secret provider registered under Kind.
func New() *Provider { return &Provider{} }

func (*Provider) Name() string { return Kind }

// Resolve returns the environment variable value for key.
func (p *Provider) Resolve(ctx context.Context, key string) (hosting.SecretValue, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil, fmt.Errorf("%w: %s", hosting.ErrSecretNotFound, key)
	}
	if value == "" {
		return nil, fmt.Errorf("%w: %s", hosting.ErrSecretEmpty, key)
	}
	return hosting.StringSecret(value), nil
}
