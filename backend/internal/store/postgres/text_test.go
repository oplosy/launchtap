package postgres

import (
	"math/big"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Contictus/launchtap/backend/internal/ledger"
)

func TestStorableTextMakesChainTextStorable(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Pons":     "Pons",
		"A\x00B":   "A�B",
		"A\xffB":   "A�B",
		"émoji 🚀":  "émoji 🚀",
		"\x00\x00": "��",
		"bad\xc3":  "bad�",
	}
	for in, want := range cases {
		if got := storableText(in); got != want {
			t.Errorf("storableText(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("a", MaxStoredTextBytes-1) + "é" + strings.Repeat("b", 3000)
	got := storableText(long)
	if len(got) > MaxStoredTextBytes || !utf8.ValidString(got) || got != strings.Repeat("a", MaxStoredTextBytes-1) {
		t.Fatalf("long text cut to %d bytes (valid %v)", len(got), utf8.ValidString(got))
	}
}

// Insert and matches-event parameters must carry the same normalized value, so a replay of an
// event with unstorable text still matches its stored row.
func TestTokenLaunchParamsNormalizeText(t *testing.T) {
	t.Parallel()
	one := big.NewInt(1)
	event := ledger.TokenLaunch{Name: "A\x00B", Symbol: "S\xff", TotalSupply: one, VirtualETH: one, VirtualToken: one,
		CurveTokens: one, LPTokens: one, GraduationETH: one, LaunchFeePaid: one}
	arg, err := tokenLaunchParams(event)
	if err != nil {
		t.Fatal(err)
	}
	if arg.Name != "A�B" || arg.Symbol != "S�" {
		t.Fatalf("params name/symbol = %q/%q", arg.Name, arg.Symbol)
	}
}
