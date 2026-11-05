package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/pagination"
)

func TestEscapeLikePatternMakesWildcardsLiteral(t *testing.T) {
	for input, want := range map[string]string{
		"pep":     "pep",
		"%":       `\%`,
		"a_b":     `a\_b`,
		`c\d%_`:   `c\\d\%\_`,
		"0xabc12": "0xabc12",
	} {
		if got := escapeLikePattern(input); got != want {
			t.Errorf("escapeLikePattern(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCursorFilterAllIsByteIdenticalToPreAgeHash(t *testing.T) {
	// "all" must not add a part: cursors minted before the age filter keep validating.
	want := cursorFilter("curve", "abc")
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%d:%s%d:%s", len("curve"), "curve", len("abc"), "abc")
	if want != hex.EncodeToString(h.Sum(nil)) {
		t.Fatal("cursorFilter changed for the legacy two-part input")
	}
	if cursorFilter("curve", "abc", "24h") == want || cursorFilter("curve", "abc", "7d") == cursorFilter("curve", "abc", "24h") {
		t.Fatal("age must change the filter hash")
	}
}

func TestAgeCutoff(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 30, 15, 999, time.FixedZone("x", 3600))
	since, arg, err := ageCutoff("24h", now, nil)
	if err != nil || since != "2026-11-04T11:30:15Z" || !arg.Valid || !arg.Time.Equal(time.Date(2026, 11, 4, 11, 30, 15, 0, time.UTC)) {
		t.Fatalf("24h since=%q arg=%v err=%v", since, arg, err)
	}
	since, _, err = ageCutoff("7d", now, nil)
	if err != nil || since != "2026-10-29T11:30:15Z" {
		t.Fatalf("7d since=%q err=%v", since, err)
	}
	since, arg, err = ageCutoff("all", now, nil)
	if err != nil || since != "" || arg.Valid {
		t.Fatalf("all since=%q arg=%v err=%v", since, arg, err)
	}
	// Later pages reuse the pinned cutoff and ignore now.
	pinned := &pagination.Cursor{Since: "2026-01-02T03:04:05Z"}
	since, arg, err = ageCutoff("24h", now, pinned)
	if err != nil || since != "2026-01-02T03:04:05Z" || !arg.Time.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("pinned since=%q arg=%v err=%v", since, arg, err)
	}
	for _, bad := range []string{"", "yesterday"} {
		if _, _, err := ageCutoff("24h", now, &pagination.Cursor{Since: bad}); !errors.Is(err, pagination.ErrInvalidCursor) {
			t.Fatalf("since %q err=%v", bad, err)
		}
	}
	if _, _, err := ageCutoff("all", now, &pagination.Cursor{Since: "2026-01-02T03:04:05Z"}); !errors.Is(err, pagination.ErrInvalidCursor) {
		t.Fatalf("all with since err=%v", err)
	}
}
