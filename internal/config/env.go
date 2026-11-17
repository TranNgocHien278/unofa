package config

import (
	"os"
	"regexp"
)

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces ${VAR} with the environment value, leaving unknown refs empty.
func ExpandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1] // slide off ${ and }
		return os.Getenv(name)
	})
}
