package hyperproviders

import (
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
	if _, err := ss.Get("missing"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrSecretNotFound", err)
	}

	if err := ss.Require("a", "b"); err != nil {
		t.Fatalf("Require: %v", err)
	}
	if err := ss.Require("a", "c"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Require missing err = %v, want ErrSecretNotFound", err)
	}
}

func TestSecret_LookupKeyReturnsKey(t *testing.T) {
	secret := &Secret{Name: "token", Key: "DEPLOY_TOKEN"}
	if secret.LookupKey() != "DEPLOY_TOKEN" {
		t.Errorf("LookupKey = %q, want DEPLOY_TOKEN", secret.LookupKey())
	}
}

func TestSecrets_ValidateRequiresNameAndKey(t *testing.T) {
	if err := (Secrets{{Name: "token", Key: "DEPLOY_TOKEN"}}).Validate(); err != nil {
		t.Fatalf("Validate complete secret: %v", err)
	}

	if err := (Secrets{{Key: "DEPLOY_TOKEN"}}).Validate(); !errors.Is(err, ErrSecretMissingName) {
		t.Fatalf("Validate missing name err = %v, want ErrSecretMissingName", err)
	}

	if err := (Secrets{{Name: "token"}}).Validate(); !errors.Is(err, ErrSecretMissingKey) {
		t.Fatalf("Validate missing key err = %v, want ErrSecretMissingKey", err)
	}
}

func TestValue_BytesCopies(t *testing.T) {
	source := []byte("secret")
	value := BytesSecret(source)
	source[0] = 'S'

	got := value.Bytes()
	got[1] = 'E'

	if value.String() != "secret" {
		t.Errorf("Value mutated to %q", value.String())
	}
}
