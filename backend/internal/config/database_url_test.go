package config

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveDatabaseURLPassword(t *testing.T) {
	t.Parallel()
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	got, err := resolveDatabaseURL(env(map[string]string{
		"DATABASE_URL":      "postgres://launchpad_api@postgres:5432/launchpad?sslmode=disable",
		"DATABASE_PASSWORD": "p@ss/word",
	}))
	if err != nil || got != "postgres://launchpad_api:p%40ss%2Fword@postgres:5432/launchpad?sslmode=disable" {
		t.Fatalf("resolveDatabaseURL() = %q, %v", got, err)
	}
	plain := "postgres://owner:pw@postgres:5432/launchpad"
	if got, err := resolveDatabaseURL(env(map[string]string{"DATABASE_URL": plain})); err != nil || got != plain {
		t.Fatalf("without a password = %q, %v; want the URL unchanged", got, err)
	}
	for _, test := range []struct {
		name     string
		url      string
		password string
		field    string
	}{
		{"URL already has a password", plain, "p@ss", "DATABASE_URL"},
		{"URL names no role", "postgres://postgres:5432/launchpad", "p@ss", "DATABASE_URL"},
		{"multi-line password", "postgres://launchpad_api@postgres:5432/launchpad", "p@ss\nb", "DATABASE_PASSWORD"},
	} {
		_, err := resolveDatabaseURL(env(map[string]string{"DATABASE_URL": test.url, "DATABASE_PASSWORD": test.password}))
		var field *FieldError
		if !errors.As(err, &field) || field.Field != test.field {
			t.Fatalf("%s: error = %v, want a %s field error", test.name, err, test.field)
		}
		if strings.Contains(err.Error(), "p@ss") || strings.Contains(err.Error(), ":pw@") {
			t.Fatalf("%s: error leaks a password: %v", test.name, err)
		}
	}
}
