package hyperproviders

import "github.com/herdworks/hyperproviders-go/secrets"

type Secret = secrets.Secret
type Secrets = secrets.Secrets
type SecretProvider = secrets.Provider
type SecretProviderBase = secrets.ProviderBase
type SecretValue = secrets.Value
type SecretValues = secrets.Values

func StringSecret(value string) SecretValue {
	return secrets.String(value)
}

func BytesSecret(value []byte) SecretValue {
	return secrets.Bytes(value)
}
