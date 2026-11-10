package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Contictus/launchtap/backend/internal/social"
	"github.com/ethereum/go-ethereum/common"
)

const scopeFlags = "-chain-id=46630 -deployment-id=dep-a -operator=ops"

func argv(command, rest string) []string {
	return strings.Fields(command + " " + scopeFlags + " " + rest)
}

// noEnv fails the test if a parse error path reaches configuration or the database.
func noEnv(t *testing.T) func(string) string {
	t.Helper()
	return func(key string) string {
		t.Errorf("getenv(%q) called before argument validation finished", key)
		return ""
	}
}

func TestParseModerateFlags(t *testing.T) {
	t.Parallel()
	bad := map[string][]string{
		"no command":            nil,
		"unknown command":       argv("purge", "-comment=1 -reason=x"),
		"missing operator":      strings.Fields("hide -chain-id=1 -deployment-id=d -comment=1 -reason=x"),
		"missing chain id":      strings.Fields("hide -deployment-id=d -operator=ops -comment=1 -reason=x"),
		"missing deployment id": strings.Fields("hide -chain-id=1 -operator=ops -comment=1 -reason=x"),
		"zero chain id":         strings.Fields("hide -chain-id=0 -deployment-id=d -operator=ops -comment=1 -reason=x"),
		"operator too long":     strings.Fields("hide -chain-id=1 -deployment-id=d -operator=" + strings.Repeat("o", 65) + " -comment=1 -reason=x"),
		"hide without reason":   argv("hide", "-comment=1"),
		"hide without comment":  argv("hide", "-reason=x"),
		"reason too long":       argv("hide", "-comment=1 -reason="+strings.Repeat("r", 501)),
		"ban both targets":      argv("ban", "-did=did:a -comment=1 -reason=x"),
		"ban no target":         argv("ban", "-reason=x"),
		"unban by comment":      argv("unban", "-comment=1 -reason=x"),
		"bad resolution":        argv("resolve", "-comment=1 -resolution=maybe -reason=x"),
		"missing resolution":    argv("resolve", "-comment=1 -reason=x"),
		"zero limit":            argv("reports", "-limit=0"),
		"stray argument":        argv("reports", "extra"),
		"unknown flag":          argv("reports", "-comment=1"),
	}
	for name, args := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			if err := execute(context.Background(), args, noEnv(t), &out); err == nil {
				t.Fatalf("execute(%v) accepted invalid input", args)
			}
			if out.Len() != 0 {
				t.Fatalf("invalid input wrote output: %q", out.String())
			}
		})
	}

	good := map[string]command{
		"reports": {Name: "reports", Limit: 50},
		"hide":    {Name: "hide", CommentID: 7, Reason: "spam"},
	}
	for name, want := range good {
		rest := ""
		switch name {
		case "hide":
			rest = "-comment=7 -reason=spam"
		}
		got, err := parseCommand(argv(name, rest))
		if err != nil {
			t.Fatalf("parseCommand(%s): %v", name, err)
		}
		want.Scope = social.Scope{ChainID: 46630, DeploymentID: "dep-a"}
		want.Operator = "ops"
		if got.Name != want.Name || got.Scope != want.Scope || got.Operator != want.Operator || got.Limit != want.Limit || got.CommentID != want.CommentID || got.Reason != want.Reason {
			t.Fatalf("parseCommand(%s) = %+v; want %+v", name, got, want)
		}
	}
	ban, err := parseCommand(argv("ban", "-comment=9 -reason=scam"))
	if err != nil || ban.CommentID != 9 || ban.DID != "" {
		t.Fatalf("ban by comment = %+v, %v", ban, err)
	}
	res, err := parseCommand(argv("resolve", "-comment=9 -resolution=dismissed -reason=ok"))
	if err != nil || res.Resolution != social.ResolutionDismissed {
		t.Fatalf("resolve = %+v, %v", res, err)
	}
}

type fakeModerator struct {
	calls   []string
	reports []social.ReportSummary
	tokens  []common.Address
	err     error
	n       int64
}

func (f *fakeModerator) record(s string) { f.calls = append(f.calls, s) }

func (f *fakeModerator) OpenReports(_ context.Context, _ social.Scope, limit int) ([]social.ReportSummary, error) {
	f.record("reports")
	return f.reports, f.err
}
func (f *fakeModerator) Hide(_ context.Context, _ social.Scope, id int64, op, reason string) error {
	f.record("hide:" + op + ":" + reason)
	return f.err
}
func (f *fakeModerator) Unhide(_ context.Context, _ social.Scope, id int64, op, reason string) error {
	f.record("unhide")
	return f.err
}
func (f *fakeModerator) Ban(_ context.Context, _ social.Scope, did, op, reason string) ([]common.Address, error) {
	f.record("ban:" + did)
	return f.tokens, f.err
}
func (f *fakeModerator) BanCommentAuthor(_ context.Context, _ social.Scope, id int64, op, reason string) ([]common.Address, error) {
	f.record("banc")
	return f.tokens, f.err
}
func (f *fakeModerator) Unban(_ context.Context, _ social.Scope, did, op, reason string) ([]common.Address, error) {
	f.record("unban:" + did)
	return f.tokens, f.err
}
func (f *fakeModerator) Resolve(_ context.Context, _ social.Scope, id int64, r social.Resolution, op, reason string) (int64, error) {
	f.record("resolve:" + string(r))
	return f.n, f.err
}

func TestRunDispatchesAndPrints(t *testing.T) {
	t.Parallel()
	f := &fakeModerator{tokens: []common.Address{common.HexToAddress("0x01")}, n: 3}
	cases := []struct {
		args []string
		call string
		want string
	}{
		{argv("hide", "-comment=4 -reason=spam"), "hide:ops:spam", "hid comment 4"},
		{argv("unhide", "-comment=4 -reason=oops"), "unhide", "unhid comment 4"},
		{argv("ban", "-did=did:x -reason=scam"), "ban:did:x", "1 token"},
		{argv("ban", "-comment=4 -reason=scam"), "banc", "1 token"},
		{argv("unban", "-did=did:x -reason=appeal"), "unban:did:x", "1 token"},
		{argv("resolve", "-comment=4 -resolution=actioned -reason=done"), "resolve:actioned", "resolved 3 report"},
	}
	for _, tc := range cases {
		cmd, err := parseCommand(tc.args)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := run(context.Background(), cmd, f, &out); err != nil {
			t.Fatalf("run %s: %v", cmd.Name, err)
		}
		if f.calls[len(f.calls)-1] != tc.call || !strings.Contains(out.String(), tc.want) {
			t.Fatalf("%s: last call %q, output %q", cmd.Name, f.calls[len(f.calls)-1], out.String())
		}
	}

	f.err = social.ErrNotFound
	cmd, _ := parseCommand(argv("hide", "-comment=4 -reason=spam"))
	var out bytes.Buffer
	if err := run(context.Background(), cmd, f, &out); !errors.Is(err, social.ErrNotFound) || out.Len() != 0 {
		t.Fatalf("not found: err=%v out=%q", err, out.String())
	}
}

func TestReportsOutputEscapesControlCharacters(t *testing.T) {
	t.Parallel()
	f := &fakeModerator{reports: []social.ReportSummary{{
		CommentID: 12, Token: common.HexToAddress("0xabc"), CreatedAt: time.Date(2026, 11, 9, 1, 2, 3, 0, time.UTC),
		Body: "line\x1b[31mred\nnext", AuthorDID: "did:a", Wallet: common.HexToAddress("0xdef"), Reports: 2,
		Reasons: []social.ReportReason{social.ReasonScam, social.ReasonSpam},
	}}}
	cmd, _ := parseCommand(argv("reports", ""))
	var out bytes.Buffer
	if err := run(context.Background(), cmd, f, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.ContainsRune(text, '\x1b') || strings.Count(text, "\n") != 1 {
		t.Fatalf("output has raw control characters: %q", text)
	}
	for _, want := range []string{"12", "scam,spam", "did:a", `\x1b[31mred\nnext`} {
		if !strings.Contains(text, want) {
			t.Fatalf("output %q lacks %q", text, want)
		}
	}
}
