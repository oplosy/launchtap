package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

// resolveDatabaseURL validates DATABASE_URL and, when DATABASE_PASSWORD_FILE is set, inserts the
// password read from that file. Each Compose service then sets a password-less URL for its own
// role and mounts only its own secret (docs/specs/2026-11-16-postgres-service-roles-preflight.md).
// Errors never include the URL or the password.
func resolveDatabaseURL(getenv func(string) string) (string, error) {
	raw := strings.TrimSpace(getenv("DATABASE_URL"))
	if err := validateURL("DATABASE_URL", raw, "postgres", "postgresql"); err != nil {
		return "", err
	}
	path := strings.TrimSpace(getenv("DATABASE_PASSWORD_FILE"))
	if path == "" {
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil || parsed.User.Username() == "" {
		return "", &FieldError{Field: "DATABASE_URL", Err: errors.New("must name the role when DATABASE_PASSWORD_FILE is set")}
	}
	if _, set := parsed.User.Password(); set {
		return "", &FieldError{Field: "DATABASE_URL", Err: errors.New("must not carry a password when DATABASE_PASSWORD_FILE is set")}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", &FieldError{Field: "DATABASE_PASSWORD_FILE", Err: errors.New("cannot be read")}
	}
	// Secret files usually end with one newline; any other line break is a malformed secret.
	password := strings.TrimSuffix(strings.TrimSuffix(string(content), "\n"), "\r")
	if password == "" || strings.ContainsAny(password, "\r\n") {
		return "", &FieldError{Field: "DATABASE_PASSWORD_FILE", Err: ErrInvalid}
	}
	parsed.User = url.UserPassword(parsed.User.Username(), password)
	return parsed.String(), nil
}
