package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDatabaseURLPasswordFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	secret := write("api", "p@ss/word\n")
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	got, err := resolveDatabaseURL(env(map[string]string{
		"DATABASE_URL":           "postgres://launchpad_api@postgres:5432/launchpad?sslmode=disable",
		"DATABASE_PASSWORD_FILE": secret,
	}))
	if err != nil || got != "postgres://launchpad_api:p%40ss%2Fword@postgres:5432/launchpad?sslmode=disable" {
		t.Fatalf("resolveDatabaseURL() = %q, %v", got, err)
	}
	plain := "postgres://owner:pw@postgres:5432/launchpad"
	if got, err := resolveDatabaseURL(env(map[string]string{"DATABASE_URL": plain})); err != nil || got != plain {
		t.Fatalf("without a password file = %q, %v; want the URL unchanged", got, err)
	}
	for _, test := range []struct {
		name  string
		url   string
		file  string
		field string
	}{
		{"URL already has a password", plain, secret, "DATABASE_URL"},
		{"URL names no role", "postgres://postgres:5432/launchpad", secret, "DATABASE_URL"},
		{"missing file", "postgres://launchpad_api@postgres:5432/launchpad", filepath.Join(dir, "absent"), "DATABASE_PASSWORD_FILE"},
		{"empty file", "postgres://launchpad_api@postgres:5432/launchpad", write("empty", "\n"), "DATABASE_PASSWORD_FILE"},
		{"multi-line file", "postgres://launchpad_api@postgres:5432/launchpad", write("multi", "a\nb\n"), "DATABASE_PASSWORD_FILE"},
	} {
		_, err := resolveDatabaseURL(env(map[string]string{"DATABASE_URL": test.url, "DATABASE_PASSWORD_FILE": test.file}))
		var field *FieldError
		if !errors.As(err, &field) || field.Field != test.field {
			t.Fatalf("%s: error = %v, want a %s field error", test.name, err, test.field)
		}
		if strings.Contains(err.Error(), "p@ss") || strings.Contains(err.Error(), ":pw@") {
			t.Fatalf("%s: error leaks a password: %v", test.name, err)
		}
	}
}
