package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/herdworks/hyperproviders-go/secrets"
)

func TestProvider_Resolve(t *testing.T) {
	provider := New()
	value, err := provider.Resolve(context.Background(), secrets.Values{
		"TEST": secrets.String("secret"),
	}, "TEST")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if value.String() != "secret" {
		t.Errorf("Resolve = %q, want secret", value.String())
	}
}

func TestProvider_ResolveMissing(t *testing.T) {
	provider := New()
	_, err := provider.Resolve(context.Background(), nil, "TEST")
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("Resolve err = %v, want ErrNotFound", err)
	}
}
