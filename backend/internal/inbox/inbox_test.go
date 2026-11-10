package inbox

import (
	"errors"
	"math"
	"testing"
)

func TestPositionRoundTrip(t *testing.T) {
	for _, p := range []Position{{0, 0, 0}, {12, 3, 4}, {math.MaxInt64, math.MaxInt32, math.MaxInt32}} {
		got, err := ParsePosition(p.String())
		if err != nil || got != p {
			t.Fatalf("%v -> %q -> %v, %v", p, p.String(), got, err)
		}
	}
}

func TestParsePositionRejects(t *testing.T) {
	for _, raw := range []string{"", "1-2", "1-2-3-4", "-1-2-3", "1--3", "01-2-3", "1-2-x", "+1-2-3", "1-2147483648-0",
		"1-2-3 ", "99999999999999999999-0-0", "1_2_3"} {
		if _, err := ParsePosition(raw); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("%q accepted (err %v)", raw, err)
		}
	}
}
