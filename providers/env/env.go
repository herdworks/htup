// Package env implements a secrets.Provider backed by OS environment variables.
package env

import (
	"context"
	"fmt"
	"os"

	"github.com/herdworks/hyperproviders-go/secrets"
)

const Kind = "env"

// Provider resolves secrets from OS environment variables.
type Provider struct {
	*secrets.ProviderBase
}

// New returns an env secret provider registered under Kind.
func New() *Provider {
	return &Provider{
		ProviderBase: &secrets.ProviderBase{ProviderName: Kind},
	}
}

// Resolve returns the environment variable value for key.
func (p *Provider) Resolve(ctx context.Context, values secrets.Values, key string) (secrets.Value, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil, fmt.Errorf("%w: %s", secrets.ErrNotFound, key)
	}
	if value == "" {
		return nil, fmt.Errorf("%w: %s", secrets.ErrEmpty, key)
	}
	return secrets.String(value), nil
}
