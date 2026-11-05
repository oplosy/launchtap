package pricefeed

import (
	"encoding/json"
	"testing"
)

func TestParsePrice(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"3456.12", "3456.12", false},
		{"3456", "3456", false},
		{"0.000000000000000001", "0.000000000000000001", false},
		{"0.0000000000000000001", "", true},
		{"0", "", true},
		{"0.0", "", true},
		{"-1", "", true},
		{"+1", "", true},
		{"3.1e3", "", true},
		{"3.1E3", "", true},
		{"", "", true},
		{"01", "", true},
		{"1.", "", true},
	}
	for _, tc := range tests {
		got, err := ParsePrice(json.Number(tc.in))
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ParsePrice(%q) = %q, %v; want %q, err=%v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}
