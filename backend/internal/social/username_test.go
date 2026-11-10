package social

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeUsernameVectors(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"Alice", "alice", nil},
		{"  Alice  ", "alice", nil},
		{"admin_fan", "admin_fan", nil},
		{"abc", "abc", nil},
		{strings.Repeat("a", 20), strings.Repeat("a", 20), nil},
		{"ab", "", ErrInvalidUsername},
		{strings.Repeat("a", 21), "", ErrInvalidUsername},
		{"", "", ErrInvalidUsername},
		{"al ice", "", ErrInvalidUsername},
		{"al-ice", "", ErrInvalidUsername},
		{"ålice", "", ErrInvalidUsername},
		{"0xabc", "", ErrInvalidUsername},
		{"0XABC", "", ErrInvalidUsername},
		{"a0xbc", "a0xbc", nil},
		{"admin", "", ErrUsernameReserved},
		{"Admin_1", "", ErrUsernameReserved},
		{"support2", "", ErrUsernameReserved},
		{"mod__", "", ErrUsernameReserved},
		{"a_d_m_i_n", "", ErrUsernameReserved},
	}
	for _, tc := range cases {
		got, err := NormalizeUsername(tc.in)
		if !errors.Is(err, tc.wantErr) || got != tc.want {
			t.Errorf("NormalizeUsername(%q) = %q, %v; want %q, %v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestReservedWordsAllRejected(t *testing.T) {
	for _, w := range strings.Fields("admin administrator launchpad support moderator mod official team staff system root help security robinhood pons null undefined anonymous deleted operator") {
		if len(w) < 3 {
			continue
		}
		if _, err := NormalizeUsername(w); !errors.Is(err, ErrUsernameReserved) {
			t.Errorf("%q err = %v; want reserved", w, err)
		}
	}
}
