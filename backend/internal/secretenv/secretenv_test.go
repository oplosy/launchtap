package secretenv

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLookupReadsSecretFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	secret := filepath.Join(dir, "api")
	if err := os.WriteFile(secret, []byte("p@ss/word\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	getenv, err := Lookup(env(map[string]string{"DATABASE_PASSWORD_FILE": secret, "OTHER": "x"}), "DATABASE_PASSWORD")
	if err != nil || getenv("DATABASE_PASSWORD") != "p@ss/word" || getenv("OTHER") != "x" {
		t.Fatalf("Lookup() password=%q other=%q err=%v", getenv("DATABASE_PASSWORD"), getenv("OTHER"), err)
	}
	if getenv, err := Lookup(env(map[string]string{"DATABASE_PASSWORD": "direct"}), "DATABASE_PASSWORD"); err != nil || getenv("DATABASE_PASSWORD") != "direct" {
		t.Fatalf("without a file: err=%v", err)
	}
	if _, err := Lookup(env(map[string]string{"DATABASE_PASSWORD": "plain-secret", "DATABASE_PASSWORD_FILE": secret}), "DATABASE_PASSWORD"); !errors.Is(err, ErrConflict) || strings.Contains(err.Error(), "plain-secret") {
		t.Fatalf("both set: err=%v, want a conflict without the value", err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(env(map[string]string{"DATABASE_PASSWORD_FILE": empty}), "DATABASE_PASSWORD"); err == nil {
		t.Fatal("empty file: want an error")
	}
	if _, err := Lookup(env(map[string]string{"DATABASE_PASSWORD_FILE": filepath.Join(dir, "absent")}), "DATABASE_PASSWORD"); err == nil || !strings.Contains(err.Error(), "DATABASE_PASSWORD_FILE") {
		t.Fatalf("missing file: err=%v", err)
	}
}
