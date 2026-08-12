// Package runtime implements a secrets provider backed by caller-supplied
// runtime secret values.
package runtime

import (
	"context"
	"fmt"

	"github.com/herdworks/htup"
)

const Kind = "runtime"

// Secrets are sensitive, caller-supplied values for one deployment or
// readiness operation.
type Secrets map[string]htup.SecretValue

// Provider resolves secret keys from the runtime values supplied at creation.
type Provider struct {
	secrets Secrets
}

// New returns a runtime secrets provider. It copies the supplied values so
// callers cannot alter the provider's values after creating it.
func New(secrets Secrets) *Provider {
	runtimeSecrets := make(Secrets, len(secrets))
	for key, value := range secrets {
		runtimeSecrets[key] = htup.BytesSecret(value)
	}
	return &Provider{secrets: runtimeSecrets}
}

func (*Provider) Name() string { return Kind }

// Resolve returns the caller-supplied value for key.
func (p *Provider) Resolve(ctx context.Context, key string) (htup.SecretValue, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: %s", htup.ErrSecretNotFound, key)
	}
	value, ok := p.secrets[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", htup.ErrSecretNotFound, key)
	}
	if value.Empty() {
		return nil, fmt.Errorf("%w: %s", htup.ErrSecretEmpty, key)
	}
	return htup.BytesSecret(value), nil
}
