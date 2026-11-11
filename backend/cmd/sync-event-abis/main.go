package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// syncPair copies one single-sourced contracts directory into the backend. A destination may
// contain another pair's destination (engine v2 copies nest under v1's); that subtree belongs
// to the other pair and is skipped when this pair is compared.
type syncPair struct {
	source, destination string
}

var repositoryPairs = []syncPair{
	{source: "../contracts/abi/v1", destination: "internal/chain/abi/v1"},
	{source: "../contracts/fixtures/v1", destination: "internal/chain/testdata"},
	{source: "../contracts/abi/v2", destination: "internal/chain/abi/v2"},
	{source: "../contracts/fixtures/v2", destination: "internal/chain/testdata/v2"},
}

func main() {
	check := flag.Bool("check", false, "fail unless the destination is byte-identical to the source")
	flag.Parse()

	if err := syncPairs(repositoryPairs, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func syncPairs(pairs []syncPair, check bool) error {
	for _, pair := range pairs {
		if !check {
			if err := copyDirectory(pair.source, pair.destination); err != nil {
				return err
			}
		}
	}
	for _, pair := range pairs {
		if err := compareDirectories(pair.source, pair.destination, nestedDestinations(pair, pairs)); err != nil {
			return err
		}
	}
	return nil
}

func nestedDestinations(pair syncPair, pairs []syncPair) []string {
	nested := make([]string, 0)
	for _, other := range pairs {
		relative, err := filepath.Rel(pair.destination, other.destination)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		nested = append(nested, filepath.ToSlash(relative))
	}
	return nested
}

func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, sourcePath)
		if err != nil {
			return err
		}
		destinationPath := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(destinationPath, 0o755)
		}
		body, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		return os.WriteFile(destinationPath, body, 0o644)
	})
}

func compareDirectories(source, destination string, skip []string) error {
	sourceFiles, err := readDirectory(source, nil)
	if err != nil {
		return fmt.Errorf("read source event-ABI: %w", err)
	}
	destinationFiles, err := readDirectory(destination, skip)
	if err != nil {
		return fmt.Errorf("read backend event-ABI copy: %w", err)
	}
	sourceNames := sortedNames(sourceFiles)
	destinationNames := sortedNames(destinationFiles)
	if !slices.Equal(sourceNames, destinationNames) {
		return fmt.Errorf("event-ABI file set differs (%s): source %v, destination %v", destination, sourceNames, destinationNames)
	}
	for _, name := range sourceNames {
		if !bytes.Equal(sourceFiles[name], destinationFiles[name]) {
			return fmt.Errorf("event-ABI file differs: %s", filepath.ToSlash(filepath.Join(destination, name)))
		}
	}
	return nil
}

func readDirectory(root string, skip []string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if slices.Contains(skip, relative) {
				return filepath.SkipDir
			}
			return nil
		}
		body, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		files[relative] = body
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("directory %q does not exist", root)
	}
	return files, err
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
