package hyperproviders

import (
	"fmt"
	"os"
)

var (
	environmentVariableSecretProvider = "env"
)

type Secret struct {
	Name string `json:"name" yaml:"name"`
	Provider string `json:"provider" yaml:"provider"`
	Key string `json:"key" yaml:"key"`
	UnsafeValue string `json:"unsafe_value,omitempty" yaml:"unsafe_value,omitempty"`
}
type Secrets []Secret

func (secret *Secret) Value() (value string, err error) {
	if len(secret.UnsafeValue) != 0 { return secret.UnsafeValue, nil }
	switch secret.Provider {
		case environmentVariableSecretProvider:
			return os.Getenv(secret.Key), nil
		default:
			return "", fmt.Errorf("unsupported secret provider: %s", secret.Provider)
	}
}

func (secrets Secrets) Get(name string) (*Secret) {
	for _, secret := range secrets {
		if secret.Name == name { return &secret }
	}
	return nil
}

func (secrets Secrets) Validate(keys ...string) (err error) {
	for _, key := range keys {
		if secrets.Get(key) == nil {
			return fmt.Errorf("missing \"%s\" secret", key)
		}
		if _,err = secrets.Get(key).Value(); err != nil {
			return fmt.Errorf("error getting \"%s\" secret value: %s", key, err)
		}
	}
	return nil
}