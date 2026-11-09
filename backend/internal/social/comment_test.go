package social

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestNormalizeBodyVectors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		rule string
	}{
		{"trim", "  hi  ", "hi", ""},
		{"crlf", "a\r\nb", "a\nb", ""},
		{"lone cr", "a\rb", "a\nb", ""},
		{"four newlines", "a\n\n\n\nb", "", "newlines"},
		{"three newlines", "a\n\n\nb", "a\n\n\nb", ""},
		{"nul", "\x00", "", "control"},
		{"tab", "\t", "", "control"},
		{"tab inside", "a\tb", "", "control"},
		{"line separator", "a\u2028b", "", "control"},
		{"paragraph separator", "a\u2029b", "", "control"},
		{"lrm", "a\u200eb", "", "control"},
		{"rlm", "a\u200fb", "", "control"},
		{"arabic letter mark", "a\u061cb", "", "control"},
		{"zero width space", "a\u200bb", "", "control"},
		{"bom inside", "a\ufeffb", "", "control"},
		{"zwnj kept", "a\u200cb", "a\u200cb", ""},
		{"zwj kept", "a\u200db", "a\u200db", ""},
		{"bidi override", "a\u202eb", "", "bidi"},
		{"bidi isolate", "a\u2066b", "", "bidi"},
		{"bidi range edge", "a\u2069b", "", "bidi"},
		{"invalid utf8", "\xff", "", "utf8"},
		{"500 two-byte runes", strings.Repeat("é", 500), strings.Repeat("é", 500), ""},
		{"501 runes", strings.Repeat("a", 501), "", "too_long"},
		{"500 emoji", strings.Repeat("😀", 500), strings.Repeat("😀", 500), ""},
		{"blank", "   ", "", "empty"},
		{"empty", "", "", "empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeBody(tc.in)
			if tc.rule == "" {
				if err != nil || got != tc.want {
					t.Fatalf("NormalizeBody = %q, %v; want %q", got, err, tc.want)
				}
				return
			}
			var be *BodyError
			if !errors.As(err, &be) || be.Rule != tc.rule {
				t.Fatalf("err = %v; want BodyError rule %q", err, tc.rule)
			}
		})
	}
}

func TestCursorRoundTrip(t *testing.T) {
	for _, id := range []int64{1, 42, 1 << 40} {
		got, err := DecodeCursor(EncodeCursor(id))
		if err != nil || got != id {
			t.Fatalf("round trip %d = %d, %v", id, got, err)
		}
	}
}

func TestDecodeCursorRejects(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, in := range []string{"", "x", "c1:0", "c1:-1", "c2:5", enc("c1:0"), enc("c1:-1"), enc("c2:5"), enc("c1:"), enc("c1:1x"), enc("c1:+5")} {
		if _, err := DecodeCursor(in); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("DecodeCursor(%q) err = %v; want ErrInvalidCursor", in, err)
		}
	}
}

func TestDisplayAuthorVectors(t *testing.T) {
	name := "alice"
	wallet := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	cases := []struct {
		name       string
		username   *string
		banned     bool
		published  bool
		wantName   *string
		wantWallet bool
	}{
		{"no username, unpublished", nil, false, false, nil, true},
		{"no username, published", nil, false, true, nil, true},
		{"username, unpublished", &name, false, false, &name, false},
		{"username, published", &name, false, true, &name, true},
		{"banned with username", &name, true, true, nil, true},
		{"banned unpublished", &name, true, false, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DisplayAuthor(tc.username, tc.banned, wallet, tc.published, RoleHolder)
			if (got.Username == nil) != (tc.wantName == nil) || (got.Username != nil && *got.Username != *tc.wantName) {
				t.Fatalf("username = %v; want %v", got.Username, tc.wantName)
			}
			if (got.Wallet != nil) != tc.wantWallet || (got.Wallet != nil && *got.Wallet != wallet) {
				t.Fatalf("wallet = %v; want present=%v", got.Wallet, tc.wantWallet)
			}
			if got.Username == nil && got.Wallet == nil {
				t.Fatal("author has neither username nor wallet")
			}
			if got.Role != RoleHolder {
				t.Fatalf("role = %q", got.Role)
			}
		})
	}
}

func TestParseReportReason(t *testing.T) {
	for _, s := range []string{"spam", "scam", "abuse", "other"} {
		got, err := ParseReportReason(s)
		if err != nil || string(got) != s {
			t.Fatalf("ParseReportReason(%q) = %q, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "Spam", "spam ", "violence"} {
		if _, err := ParseReportReason(s); !errors.Is(err, ErrInvalidReport) {
			t.Fatalf("ParseReportReason(%q) err = %v; want ErrInvalidReport", s, err)
		}
	}
}

func TestRateLimitedErrorAs(t *testing.T) {
	var err error = &RateLimitedError{RetryAfter: 3_000_000_000}
	var rl *RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter != 3_000_000_000 || rl.Error() == "" {
		t.Fatalf("errors.As failed: %v", err)
	}
}
