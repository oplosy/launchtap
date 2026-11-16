// Package secretenv reads secrets that deploy mounts as files (Docker secrets) and serves them
// through the environment lookup that internal/config consumes, which stays free of file I/O.
package secretenv

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrConflict reports a secret set both directly and through its _FILE variable.
var ErrConflict = errors.New("set both directly and through a file")

// Lookup returns getenv extended so that each name whose NAME_FILE variable is set reads as the
// content of that file, with one trailing line break removed. It fails when a file cannot be
// read or is empty, or when NAME and NAME_FILE are both set. Errors name the variable, never its
// value.
func Lookup(getenv func(string) string, names ...string) (func(string) string, error) {
	values := make(map[string]string, len(names))
	for _, name := range names {
		path := strings.TrimSpace(getenv(name + "_FILE"))
		if path == "" {
			continue
		}
		if getenv(name) != "" {
			return nil, fmt.Errorf("%s: %w", name, ErrConflict)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s_FILE: cannot be read", name)
		}
		value := strings.TrimSuffix(strings.TrimSuffix(string(content), "\n"), "\r")
		if value == "" {
			return nil, fmt.Errorf("%s_FILE: is empty", name)
		}
		values[name] = value
	}
	return func(key string) string {
		if value, ok := values[key]; ok {
			return value
		}
		return getenv(key)
	}, nil
}
