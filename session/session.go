package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/herdworks/hyperproviders-go/secrets"
)

const RuntimeSecretProviderName = "runtime"

// Session carries values and provider choices supplied at deploy time.
// When SecretProviders is empty, only session-provided secrets are consulted.
type Session struct {
	Secrets         secrets.Values
	SecretProviders []secrets.Provider
}

// ResolveSecret resolves key using session provider precedence.
func (s *Session) ResolveSecret(ctx context.Context, key string) (secrets.Value, error) {
	for _, provider := range s.secretProviders() {
		value, err := provider.Resolve(ctx, s.secretValues(), key)
		if err == nil {
			return value, nil
		}
		if errors.Is(err, secrets.ErrNotFound) {
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("%w: %s", secrets.ErrNotFound, key)
}

func (s *Session) secretValues() secrets.Values {
	if s == nil {
		return nil
	}
	return s.Secrets
}

func (s *Session) secretProviders() []secrets.Provider {
	if s == nil || len(s.SecretProviders) == 0 {
		return []secrets.Provider{runtimeProvider{}}
	}
	return s.SecretProviders
}

type runtimeProvider struct{}

func (runtimeProvider) Name() string { return RuntimeSecretProviderName }

func (runtimeProvider) Resolve(ctx context.Context, values secrets.Values, key string) (secrets.Value, error) {
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
