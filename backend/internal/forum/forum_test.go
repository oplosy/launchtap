package forum

import "testing"

func TestParseSort(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Sort
		ok   bool
	}{{"hot", SortHot, true}, {"top", SortTop, true}, {"", "", false}, {"HOT", "", false}, {"new", "", false}} {
		got, ok := ParseSort(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("ParseSort(%q) = %q, %v", tc.in, got, ok)
		}
	}
}
