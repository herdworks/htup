package types

import (
	"fmt"
	"os"
	"regexp"
)

// EnvSecretProvider retrieves secrets from environment variables
type EnvSecretProvider struct{}

// GetSecret returns a secret value
func (b *EnvSecretProvider) GetSecret(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("environment variable '%s' not found or empty", key)
	}
	return value, nil
}

// Validate validates environment variable key format
func (b *EnvSecretProvider) Validate(key string) error {
	// Regular expression:
	// ^[a-zA-Z_]  - Must start with a letter (upper or lower) or an underscore
	// [a-zA-Z0-9_]* - Can be followed by zero or more letters, digits, or underscores
	// $           - End of the string
	re := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	if !re.MatchString(key) {
		return fmt.Errorf("environment variable '%s' has invalid format", key)
	}

	return nil
}