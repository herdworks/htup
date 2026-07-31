package runtime

import (
	"context"
	"errors"
	"testing"

	hyperproviders "github.com/herdworks/hyperproviders-go"
)

func TestProviderResolve(t *testing.T) {
	values := Secrets{"TEST": hyperproviders.StringSecret("secret")}
	provider := New(values)
	values["TEST"][0] = 'x'

	value, err := provider.Resolve(context.Background(), "TEST")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "secret" {
		t.Errorf("Resolve = %q, want secret", value.String())
	}
}

func TestProviderResolveMissing(t *testing.T) {
	_, err := New(nil).Resolve(context.Background(), "TEST")
	if !errors.Is(err, hyperproviders.ErrSecretNotFound) {
		t.Fatalf("Resolve err = %v, want ErrSecretNotFound", err)
	}
}

func TestProviderResolveEmpty(t *testing.T) {
	_, err := New(Secrets{"TEST": nil}).Resolve(context.Background(), "TEST")
	if !errors.Is(err, hyperproviders.ErrSecretEmpty) {
		t.Fatalf("Resolve err = %v, want ErrSecretEmpty", err)
	}
}
