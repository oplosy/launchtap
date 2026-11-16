package postgres

import (
	"strings"
	"unicode/utf8"
)

// MaxStoredTextBytes caps chain-derived token name and symbol text. The factories accept any
// string, but Postgres TEXT rejects NUL and invalid UTF-8, and the lower(name) / lower(symbol)
// btree indexes reject entries over about 2.7 kB; either error would abort the chunk and stop
// ingestion for the whole deployment.
const MaxStoredTextBytes = 512

// storableText is the single ingest rule for chain-derived token text. Insert and matches-event
// parameters both come from it, so a replayed event compares equal to its stored row. NUL and
// invalid UTF-8 become U+FFFD and the result is cut to MaxStoredTextBytes on a rune boundary.
func storableText(s string) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", "�")
	if len(s) <= MaxStoredTextBytes {
		return s
	}
	cut := MaxStoredTextBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
