package postgres

import "testing"

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
