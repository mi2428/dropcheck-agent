package harness

import (
	"fmt"
	"os"
)

// Secret is a redacted string value used for Wi-Fi passphrases.
type Secret struct {
	value string
	env   string
}

// SecretValue returns a Secret backed by value.
func SecretValue(value string) Secret {
	return Secret{value: value}
}

// SecretEnv returns a Secret loaded from an environment variable at run time.
func SecretEnv(name string) Secret {
	return Secret{env: name}
}

func (s Secret) resolve() (string, error) {
	if s.env != "" {
		value := os.Getenv(s.env)
		if value == "" {
			return "", fmt.Errorf("environment variable %s is empty", s.env)
		}
		return value, nil
	}
	if s.value == "" {
		return "", fmt.Errorf("secret is empty")
	}
	return s.value, nil
}

// Redacted returns a safe display value for logs and reports.
func (s Secret) Redacted() string {
	if s.env != "" {
		return "<" + s.env + ">"
	}
	if s.value == "" {
		return "<empty>"
	}
	return "<redacted>"
}
