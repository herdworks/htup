package hyperproviders

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// SecretsProvider resolves secret values by caller-defined key.
type SecretsProvider interface {
	Name() string
	Resolve(context.Context, string) (SecretValue, error)
}

// SecretsProviders resolves secrets through providers in precedence order.
type SecretsProviders []SecretsProvider

// Resolve returns the first provider value for key. A provider may decline a
// key with ErrSecretNotFound, allowing the next provider to try it.
func (providers SecretsProviders) Resolve(ctx context.Context, key string) (SecretValue, error) {
	for _, provider := range providers {
		value, err := provider.Resolve(ctx, key)
		if err == nil {
			return value, nil
		}
		if errors.Is(err, ErrSecretNotFound) {
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("%w: %s", ErrSecretNotFound, key)
}

type Secret struct {
	Name string `json:"name" yaml:"name"`
	Key  string `json:"key" yaml:"key"`
}

type SecretValue []byte

type Secrets []Secret

func (s *Secret) LookupKey() string {
	if s == nil {
		return ""
	}
	return s.Key
}

func (s *Secret) Validate() error {
	if s == nil || strings.TrimSpace(s.Name) == "" {
		return ErrSecretMissingName
	}
	if strings.TrimSpace(s.Key) == "" {
		return ErrSecretMissingKey
	}
	return nil
}

func (v SecretValue) String() string { return string(v) }

func (v SecretValue) Bytes() []byte { return append([]byte(nil), v...) }

func (v SecretValue) Empty() bool { return len(v) == 0 }

func (s Secrets) Get(name string) (*Secret, error) {
	for i := range s {
		if s[i].Name == name {
			return &s[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrSecretNotFound, name)
}

func (s Secrets) Validate() error {
	for i := range s {
		if err := s[i].Validate(); err != nil {
			return fmt.Errorf("secret[%d]: %w", i, err)
		}
	}
	return nil
}

func (s Secrets) Require(required ...string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	for _, name := range required {
		if _, err := s.Get(name); err != nil {
			return err
		}
	}
	return nil
}

func StringSecret(value string) SecretValue { return SecretValue([]byte(value)) }

func BytesSecret(value []byte) SecretValue {
	if len(value) == 0 {
		return nil
	}
	return append(SecretValue(nil), value...)
}
