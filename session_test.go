package htup

import (
	"context"
	"errors"
	"testing"
)

type fakeSecretProvider struct {
	name   string
	values map[string]string
}

func (p *fakeSecretProvider) Name() string { return p.name }

func (p *fakeSecretProvider) Resolve(ctx context.Context, key string) (SecretValue, error) {
	value, ok := p.values[key]
	if !ok {
		return nil, ErrSecretNotFound
	}
	if value == "" {
		return nil, ErrSecretEmpty
	}
	return StringSecret(value), nil
}

func TestSecretsProvidersResolveMissing(t *testing.T) {
	_, err := (SecretsProviders{}).Resolve(context.Background(), "X")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Resolve err = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretsProvidersResolvePrecedence(t *testing.T) {
	providers := SecretsProviders{
		&fakeSecretProvider{
			name:   "first",
			values: map[string]string{"TEST": "first"},
		},
		&fakeSecretProvider{
			name:   "second",
			values: map[string]string{"TEST": "second"},
		},
	}

	value, err := providers.Resolve(context.Background(), "TEST")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "first" {
		t.Errorf("Resolve = %q, want first", value.String())
	}
}

func TestSecretsProvidersResolveSkipsProviderNotFound(t *testing.T) {
	providers := SecretsProviders{
		&fakeSecretProvider{
			name:   "missing",
			values: map[string]string{},
		},
		&fakeSecretProvider{
			name:   "second",
			values: map[string]string{"TEST": "second"},
		},
	}

	value, err := providers.Resolve(context.Background(), "TEST")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "second" {
		t.Errorf("Resolve = %q, want second", value.String())
	}
}
