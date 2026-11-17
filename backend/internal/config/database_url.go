package config

import (
	"errors"
	"net/url"
	"strings"
)

// resolveDatabaseURL validates DATABASE_URL and, when DATABASE_PASSWORD is set, inserts it as the
// URL's password. Deploy mounts the password as a file and each binary maps DATABASE_PASSWORD_FILE
// onto DATABASE_PASSWORD (internal/secretenv), so each Compose service sets a password-less URL
// for its own role and mounts only its own secret
// (docs/specs/2026-11-16-postgres-service-roles-preflight.md). Errors never include the URL or
// the password.
func resolveDatabaseURL(getenv func(string) string) (string, error) {
	raw := strings.TrimSpace(getenv("DATABASE_URL"))
	if err := validateURL("DATABASE_URL", raw, "postgres", "postgresql"); err != nil {
		return "", err
	}
	password := getenv("DATABASE_PASSWORD")
	if password == "" {
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil || parsed.User.Username() == "" {
		return "", &FieldError{Field: "DATABASE_URL", Err: errors.New("must name the role when DATABASE_PASSWORD is set")}
	}
	if _, set := parsed.User.Password(); set {
		return "", &FieldError{Field: "DATABASE_URL", Err: errors.New("must not carry a password when DATABASE_PASSWORD is set")}
	}
	if strings.ContainsAny(password, "\r\n") {
		return "", &FieldError{Field: "DATABASE_PASSWORD", Err: ErrInvalid}
	}
	parsed.User = url.UserPassword(parsed.User.Username(), password)
	return parsed.String(), nil
}
