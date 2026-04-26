package env

import (
	"context"
	"errors"
	"testing"

	"github.com/herdworks/hyperproviders-go/secrets"
)

func TestProvider_Resolve(t *testing.T) {
	t.Setenv("TEST_HYPER_SECRET", "s3cret")

	provider := New()
	value, err := provider.Resolve(context.Background(), nil, "TEST_HYPER_SECRET")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "s3cret" {
		t.Errorf("Resolve = %q, want s3cret", value.String())
	}
}

func TestProvider_ResolveEmptyReturnsError(t *testing.T) {
	t.Setenv("TEST_HYPER_MISSING", "")

	provider := New()
	_, err := provider.Resolve(context.Background(), nil, "TEST_HYPER_MISSING")
	if !errors.Is(err, secrets.ErrEmpty) {
		t.Fatalf("Resolve err = %v, want ErrSecretEmpty", err)
	}
}
