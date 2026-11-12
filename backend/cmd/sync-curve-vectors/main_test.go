package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A v2 copy nested inside the v1 destination must not count as drift in the v1 copy.
func TestSyncPairsKeepsNestedDestinationsSeparate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "v1", "same.json"), "v1")
	writeFile(t, filepath.Join(root, "src", "v2", "same.json"), "v2")
	destination := filepath.Join(root, "dst")
	pairs := []syncPair{
		{source: filepath.Join(root, "src", "v1"), destination: destination},
		{source: filepath.Join(root, "src", "v2"), destination: filepath.Join(destination, "v2")},
	}

	if err := syncPairs(pairs, false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := syncPairs(pairs, true); err != nil {
		t.Fatalf("check after sync: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(destination, "v2", "same.json"))
	if err != nil || string(body) != "v2" {
		t.Fatalf("v2 copy = %q, %v", body, err)
	}

	writeFile(t, filepath.Join(destination, "v2", "same.json"), "drift")
	if err := syncPairs(pairs, true); err == nil || !strings.Contains(err.Error(), "same.json") {
		t.Fatalf("drifted v2 copy: got %v", err)
	}
	writeFile(t, filepath.Join(destination, "v2", "same.json"), "v2")
	writeFile(t, filepath.Join(destination, "stray.json"), "x")
	if err := syncPairs(pairs, true); err == nil || !strings.Contains(err.Error(), "stray.json") {
		t.Fatalf("stray v1 file: got %v", err)
	}
}

func TestRepositoryPairsIncludeEngineV2(t *testing.T) {
	sources := make(map[string]string)
	for _, pair := range repositoryPairs {
		sources[filepath.ToSlash(pair.source)] = filepath.ToSlash(pair.destination)
	}
	for source, destination := range wantedPairs {
		if sources[source] != destination {
			t.Errorf("pair %s -> %q, want %q", source, sources[source], destination)
		}
	}
}

// The repository copies must be in sync; this is the same check `task verify` runs.
func TestRepositoryCopiesAreByteIdentical(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	if err := syncPairs(repositoryPairs, true); err != nil {
		t.Fatal(err)
	}
}

var wantedPairs = map[string]string{
	"../contracts/vectors/v1": "internal/curve/testdata",
	"../contracts/vectors/v2": "internal/curve/testdata/v2",
}
