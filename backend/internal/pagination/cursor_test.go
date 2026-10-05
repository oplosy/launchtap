package pagination

import (
	"encoding/base64"
	"errors"
	"testing"
)

func TestCursorRoundTripAndStrictValidation(t *testing.T) {
	c := Cursor{Version: CurrentVersion, Snapshot: Snapshot{ChainID: 46630, BlockNumber: 7}, Endpoint: "tokens", Sort: "newest", Filters: "curve", Direction: "next", Key: []string{"7", "0x01"}}
	encoded, err := Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.Endpoint != c.Endpoint || got.Snapshot.ChainID != c.Snapshot.ChainID {
		t.Fatalf("round trip mismatch: %#v", got)
	}
	if _, err := Decode(encoded + "!"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("want invalid cursor, got %v", err)
	}
	unknown := base64.RawURLEncoding.EncodeToString([]byte(`{"version":1,"snapshot":{"chainId":1,"blockNumber":1,"blockHash":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]},"endpoint":"tokens","sort":"newest","filters":"","direction":"next","key":["1"],"extra":true}`))
	if _, err := Decode(unknown); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("unknown cursor field accepted: %v", err)
	}
}

func TestCursorRequestAndReorgInvalidation(t *testing.T) {
	c := Cursor{Version: CurrentVersion, Snapshot: Snapshot{ChainID: 1, BlockNumber: 2}, Endpoint: "tokens", Sort: "newest", Direction: "next", Key: []string{"2"}}
	if err := c.ValidateRequest("tokens", "newest", "", "next", c.Snapshot, nil); err != nil {
		t.Fatal(err)
	}
	changed := c.Snapshot
	changed.BlockHash[0] = 1
	if !errors.Is(c.ValidateRequest("tokens", "newest", "", "next", changed, nil), ErrCursorInvalidated) {
		t.Fatal("reorg identity was not rejected")
	}
	if !errors.Is(c.ValidateRequest("tokens", "oldest", "", "next", c.Snapshot, nil), ErrInvalidCursor) {
		t.Fatal("changed sort was not rejected")
	}
}

func TestCursorSurvivesTipAdvanceButNotReorg(t *testing.T) {
	c := Cursor{Version: CurrentVersion, Snapshot: Snapshot{ChainID: 1, BlockNumber: 2}, Endpoint: "tokens", Sort: "newest", Direction: "next", Key: []string{"2"}}
	later := Snapshot{ChainID: 1, BlockNumber: 5, BlockHash: [32]byte{5}}
	var checked Snapshot
	canonical := func(s Snapshot) (bool, error) { checked = s; return true, nil }
	if err := c.ValidateRequest("tokens", "newest", "", "next", later, canonical); err != nil {
		t.Fatalf("tip advance invalidated a canonical cursor: %v", err)
	}
	if checked != c.Snapshot {
		t.Fatalf("canonical check received %+v", checked)
	}
	orphaned := func(Snapshot) (bool, error) { return false, nil }
	if !errors.Is(c.ValidateRequest("tokens", "newest", "", "next", later, orphaned), ErrCursorInvalidated) {
		t.Fatal("orphaned cursor block was accepted")
	}
	if !errors.Is(c.ValidateRequest("tokens", "newest", "", "next", later, nil), ErrCursorInvalidated) {
		t.Fatal("older cursor accepted without a canonical check")
	}
	rolledBack := Snapshot{ChainID: 1, BlockNumber: 1}
	if !errors.Is(c.ValidateRequest("tokens", "newest", "", "next", rolledBack, canonical), ErrCursorInvalidated) {
		t.Fatal("cursor ahead of the current snapshot was accepted")
	}
	failure := errors.New("database down")
	if err := c.ValidateRequest("tokens", "newest", "", "next", later, func(Snapshot) (bool, error) { return false, failure }); !errors.Is(err, failure) {
		t.Fatalf("canonical check error was not returned: %v", err)
	}
}
