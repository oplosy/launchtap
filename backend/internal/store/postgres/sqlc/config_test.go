package sqlc

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var (
	createTablePattern = regexp.MustCompile(`(?ms)^CREATE TABLE\s+([a-z_][a-z0-9_]*)\s*\((.*?)^\);`)
	columnPattern      = regexp.MustCompile(`(?m)^\s+([a-z_][a-z0-9_]*)\s+(BYTEA|NUMERIC\(78,0\)|NUMERIC\(38,18\))([^,]*),`)
	overridePattern    = regexp.MustCompile(`(?m)^\s+- column: "([^"]+)"\r?\n\s+go_type: \{type: "(Address|Hash|Uint256)"(, pointer: true)?\}`)
	alterTablePattern  = regexp.MustCompile(`(?s)ALTER TABLE\s+([a-z_][a-z0-9_]*)\s+(.*?);`)
	dropNotNullPattern = regexp.MustCompile(`ALTER COLUMN\s+([a-z_][a-z0-9_]*)\s+DROP NOT NULL`)
	addColumnPattern   = regexp.MustCompile(`ADD COLUMN\s+([a-z_][a-z0-9_]*)\s+(BYTEA|NUMERIC\(78,0\))([^,;]*)`)
)

// signedNumericColumns hold signed V4 deltas. They stay pgtype.Numeric, because Uint256 rejects
// negative values by design.
var signedNumericColumns = map[string]bool{
	"v4_liquidity_changes.liquidity_delta": true,
	"v4_swaps.amount0_delta":               true,
	"v4_swaps.amount1_delta":               true,
}

type configuredOverride struct {
	pattern  string
	typeName string
	pointer  bool
}

func TestColumnOverridesCoverFixedWidthSchema(t *testing.T) {
	t.Parallel()

	packageDir := currentPackageDir(t)
	configBytes, err := os.ReadFile(filepath.Join(packageDir, "../../../../sqlc.yaml"))
	if err != nil {
		t.Fatalf("read sqlc config: %v", err)
	}
	overrides := parseOverrides(string(configBytes))

	migrationPaths, err := filepath.Glob(filepath.Join(packageDir, "../migrations/*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(migrationPaths)
	migrationTexts := make([]string, len(migrationPaths))
	relaxed := make(map[string]bool)
	for index, migrationPath := range migrationPaths {
		migrationBytes, readErr := os.ReadFile(migrationPath)
		if readErr != nil {
			t.Fatalf("read migration %s: %v", migrationPath, readErr)
		}
		migrationTexts[index] = string(migrationBytes)
		// A later ALTER COLUMN ... DROP NOT NULL makes a created column nullable.
		for _, alter := range alterTablePattern.FindAllStringSubmatch(upSection(migrationTexts[index]), -1) {
			for _, dropped := range dropNotNullPattern.FindAllStringSubmatch(alter[2], -1) {
				relaxed[alter[1]+"."+dropped[1]] = true
			}
		}
	}
	for _, migration := range migrationTexts {
		assertMigrationOverrides(t, migration, overrides, relaxed)
		assertAddedColumnOverrides(t, migration, overrides)
	}
}

func upSection(migration string) string {
	up, _, _ := strings.Cut(migration, "-- +goose Down")
	return up
}

// assertAddedColumnOverrides covers columns added by ALTER TABLE ... ADD COLUMN in an Up section.
func assertAddedColumnOverrides(t *testing.T, migration string, overrides []configuredOverride) {
	t.Helper()
	up := upSection(migration)
	for _, alter := range alterTablePattern.FindAllStringSubmatch(up, -1) {
		// The length CHECK of an added column may sit in a separate ALTER of the same migration.
		for _, added := range addColumnPattern.FindAllStringSubmatch(alter[2], -1) {
			assertColumnOverride(t, alter[1], added[1], added[2], added[3], up, overrides, false)
		}
	}
}

func parseOverrides(config string) []configuredOverride {
	matches := overridePattern.FindAllStringSubmatch(config, -1)
	overrides := make([]configuredOverride, 0, len(matches))
	for _, match := range matches {
		overrides = append(overrides, configuredOverride{
			pattern:  match[1],
			typeName: match[2],
			pointer:  match[3] != "",
		})
	}
	return overrides
}

func assertMigrationOverrides(t *testing.T, migration string, overrides []configuredOverride, relaxed map[string]bool) {
	t.Helper()
	for _, tableMatch := range createTablePattern.FindAllStringSubmatch(migration, -1) {
		table, body := tableMatch[1], tableMatch[2]
		for _, columnMatch := range columnPattern.FindAllStringSubmatch(body, -1) {
			column, sqlType, suffix := columnMatch[1], columnMatch[2], columnMatch[3]
			assertColumnOverride(t, table, column, sqlType, suffix, body, overrides, relaxed[table+"."+column])
		}
	}
}

// assertColumnOverride checks one fixed-width column. A NUMERIC(78,0) column is a pointer only
// once a later migration drops its NOT NULL; a BYTEA column is a pointer whenever it is nullable.
func assertColumnOverride(
	t *testing.T,
	table, column, sqlType, suffix, body string,
	overrides []configuredOverride,
	relaxed bool,
) {
	t.Helper()
	qualified := table + "." + column
	nullable := relaxed || !strings.Contains(suffix, "NOT NULL")
	switch sqlType {
	case "NUMERIC(38,18)":
		if matched, ok := matchingOverride(qualified, overrides); ok && matched.typeName == "Uint256" {
			t.Errorf("USD column %s must not use Uint256", qualified)
		}
	case "NUMERIC(78,0)":
		if signedNumericColumns[qualified] {
			if matched, ok := matchingOverride(qualified, overrides); ok {
				t.Errorf("signed column %s must stay pgtype.Numeric, got %s override", qualified, matched.typeName)
			}
			return
		}
		assertOverride(t, qualified, "Uint256", relaxed || (nullable && strings.Contains(body, "ADD COLUMN")), overrides)
	case "BYTEA":
		// Image content is intentionally variable-length binary data; the
		// migration bounds it by byte_size rather than a fixed-width codec.
		if qualified == "token_images.content" {
			return
		}
		lengthPattern := regexp.MustCompile(`octet_length\(` + regexp.QuoteMeta(column) + `\) = (20|32)`)
		lengthMatch := lengthPattern.FindStringSubmatch(body)
		if lengthMatch == nil {
			t.Errorf("BYTEA column %s has no fixed-length constraint", qualified)
			return
		}
		expectedType := "Address"
		if lengthMatch[1] == "32" {
			expectedType = "Hash"
		}
		assertOverride(t, qualified, expectedType, nullable, overrides)
	}
}

func assertOverride(
	t *testing.T,
	qualified string,
	expectedType string,
	expectedPointer bool,
	overrides []configuredOverride,
) {
	t.Helper()
	matched, ok := matchingOverride(qualified, overrides)
	if !ok {
		t.Errorf("column %s has no sqlc override", qualified)
		return
	}
	if matched.typeName != expectedType || matched.pointer != expectedPointer {
		t.Errorf(
			"column %s override = %s pointer=%t, want %s pointer=%t",
			qualified, matched.typeName, matched.pointer, expectedType, expectedPointer,
		)
	}
}

func matchingOverride(qualified string, overrides []configuredOverride) (configuredOverride, bool) {
	for _, override := range overrides {
		matched, err := path.Match(override.pattern, qualified)
		if err == nil && matched {
			return override, true
		}
	}
	return configuredOverride{}, false
}

func currentPackageDir(t testing.TB) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current package path")
	}
	return filepath.Dir(filename)
}
