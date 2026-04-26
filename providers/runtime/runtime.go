// Package runtime implements a secrets.Provider backed by session-provided
// secret values.
package runtime

import (
	"context"
	"fmt"

	"github.com/herdworks/hyperproviders-go/secrets"
	"github.com/herdworks/hyperproviders-go/session"
)

const Kind = session.RuntimeSecretProviderName

// Provider resolves secrets from the active session's secret values.
type Provider struct{}

// New returns a runtime secret provider.
func New() Provider {
	return Provider{}
}

// Name returns the provider name.
func (Provider) Name() string {
	return Kind
}

// Resolve returns key from values.
func (Provider) Resolve(ctx context.Context, values secrets.Values, key string) (secrets.Value, error) {
	if values == nil {
		return nil, fmt.Errorf("%w: %s", secrets.ErrNotFound, key)
	}
	value, ok := values[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", secrets.ErrNotFound, key)
	}
	if value.Empty() {
		return nil, fmt.Errorf("%w: %s", secrets.ErrEmpty, key)
	}
	return value, nil
}
