package secrets

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrNotFound        = errors.New("secret not found")
	ErrEmpty           = errors.New("secret resolved to empty value")
	ErrResolveRequired = errors.New("secret provider Resolve is required")
)

// Value is an opaque secret value. Use String or Bytes at the boundary where
// a concrete provider needs a specific representation.
type Value []byte

// String returns a Value from a string.
func String(value string) Value {
	return Value([]byte(value))
}

// Bytes returns a Value from bytes.
func Bytes(value []byte) Value {
	if len(value) == 0 {
		return nil
	}
	copied := make([]byte, len(value))
	copy(copied, value)
	return Value(copied)
}

// String returns the value as a string.
func (v Value) String() string {
	return string(v)
}

// Bytes returns a copy of the value bytes.
func (v Value) Bytes() []byte {
	if len(v) == 0 {
		return nil
	}
	copied := make([]byte, len(v))
	copy(copied, v)
	return copied
}

// Empty reports whether the value is empty.
func (v Value) Empty() bool {
	return len(v) == 0
}

// Values maps runtime secret keys to values.
type Values map[string]Value

// Provider defines a resolver for secret keys at deploy time.
type Provider interface {
	Name() string
	Resolve(ctx context.Context, values Values, key string) (Value, error)
}

// ProviderBase holds a provider name and a default Resolve implementation.
type ProviderBase struct {
	ProviderName string
}

// Name returns the secret provider name.
func (p *ProviderBase) Name() string {
	if p == nil {
		return ""
	}
	return p.ProviderName
}

// Resolve fails by default. Concrete secret provider packages are responsible
// for implementing provider-specific resolution.
func (p *ProviderBase) Resolve(ctx context.Context, values Values, key string) (Value, error) {
	return nil, fmt.Errorf("%w: %s", ErrResolveRequired, p.Name())
}

// Secret is a single credential declaration. It names the credential needed by
// a hosting provider and the runtime key used to resolve it.
type Secret struct {
	Name string `json:"name"          yaml:"name"`
	Key  string `json:"key,omitempty" yaml:"key,omitempty"`
}

// LookupKey returns Key when set, otherwise Name.
func (s *Secret) LookupKey() string {
	if s.Key != "" {
		return s.Key
	}
	return s.Name
}

// Secrets is a collection of Secret with lookup and validation helpers.
type Secrets []Secret

// Get returns the secret with the given name, or ErrNotFound.
func (s Secrets) Get(name string) (*Secret, error) {
	for i := range s {
		if s[i].Name == name {
			return &s[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// Require confirms every required secret declaration exists.
func (s Secrets) Require(required ...string) error {
	for _, name := range required {
		if _, err := s.Get(name); err != nil {
			return err
		}
	}
	return nil
}
