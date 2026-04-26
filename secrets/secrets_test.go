package secrets

import (
	"context"
	"errors"
	"testing"
)

func TestSecrets_GetAndRequire(t *testing.T) {
	ss := Secrets{
		{Name: "a", Key: "TEST_A"},
		{Name: "b", Key: "TEST_B"},
	}

	secret, err := ss.Get("a")
	if err != nil {
		t.Fatalf("Get(a): %v", err)
	}
	if secret.LookupKey() != "TEST_A" {
		t.Errorf("LookupKey = %q, want TEST_A", secret.LookupKey())
	}
	if _, err := ss.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}

	if err := ss.Require("a", "b"); err != nil {
		t.Fatalf("Require: %v", err)
	}
	if err := ss.Require("a", "c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Require missing err = %v, want ErrNotFound", err)
	}
}

func TestSecret_LookupKeyDefaultsToName(t *testing.T) {
	secret := &Secret{Name: "token"}
	if secret.LookupKey() != "token" {
		t.Errorf("LookupKey = %q, want token", secret.LookupKey())
	}
}

func TestProviderBase_ResolveRequiresImplementation(t *testing.T) {
	provider := &ProviderBase{ProviderName: "default"}
	_, err := provider.Resolve(context.Background(), nil, "TEST")
	if !errors.Is(err, ErrResolveRequired) {
		t.Fatalf("Resolve err = %v, want ErrResolveRequired", err)
	}
}

func TestValue_BytesCopies(t *testing.T) {
	source := []byte("secret")
	value := Bytes(source)
	source[0] = 'S'

	got := value.Bytes()
	got[1] = 'E'

	if value.String() != "secret" {
		t.Errorf("Value mutated to %q", value.String())
	}
}
