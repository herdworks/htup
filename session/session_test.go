package session

import (
	"context"
	"errors"
	"testing"

	"github.com/herdworks/hyperproviders-go/secrets"
)

type fakeSecretProvider struct {
	*secrets.ProviderBase
	values map[string]string
}

func (p *fakeSecretProvider) Resolve(ctx context.Context, values secrets.Values, key string) (secrets.Value, error) {
	value, ok := p.values[key]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	if value == "" {
		return nil, secrets.ErrEmpty
	}
	return secrets.String(value), nil
}

func TestSession_ResolveSecretDefaultsToSessionSecrets(t *testing.T) {
	session := &Session{
		Secrets: secrets.Values{"TEST_HYPER_SECRET": secrets.String("plain")},
	}
	value, err := session.ResolveSecret(context.Background(), "TEST_HYPER_SECRET")
	if err != nil {
		t.Fatalf("ResolveSecret: %v", err)
	}
	if value.String() != "plain" {
		t.Errorf("ResolveSecret = %q, want plain", value.String())
	}
}

func TestSession_ResolveSecretMissing(t *testing.T) {
	session := &Session{}
	_, err := session.ResolveSecret(context.Background(), "X")
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("ResolveSecret err = %v, want ErrNotFound", err)
	}
}

func TestSession_ResolveSecretPrecedence(t *testing.T) {
	session := &Session{
		Secrets: secrets.Values{"TEST": secrets.String("session")},
		SecretProviders: []secrets.Provider{
			&fakeSecretProvider{
				ProviderBase: &secrets.ProviderBase{ProviderName: "first"},
				values:       map[string]string{"TEST": "first"},
			},
			runtimeProvider{},
		},
	}

	value, err := session.ResolveSecret(context.Background(), "TEST")
	if err != nil {
		t.Fatalf("ResolveSecret: %v", err)
	}
	if value.String() != "first" {
		t.Errorf("ResolveSecret = %q, want first", value.String())
	}
}

func TestSession_ResolveSecretSkipsProviderNotFound(t *testing.T) {
	session := &Session{
		Secrets: secrets.Values{"TEST": secrets.String("session")},
		SecretProviders: []secrets.Provider{
			&fakeSecretProvider{
				ProviderBase: &secrets.ProviderBase{ProviderName: "missing"},
				values:       map[string]string{},
			},
			runtimeProvider{},
		},
	}

	value, err := session.ResolveSecret(context.Background(), "TEST")
	if err != nil {
		t.Fatalf("ResolveSecret: %v", err)
	}
	if value.String() != "session" {
		t.Errorf("ResolveSecret = %q, want session", value.String())
	}
}
