package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestParseFlagsValidation(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-rate=0"}, {"-batch=0"}, {"-batch=101"}, {"-rate=-1"}} {
		if _, err := parseFlags(args); err == nil {
			t.Fatalf("parseFlags(%v) accepted invalid input", args)
		}
	}
	got, err := parseFlags(nil)
	if err != nil || got.Rate != 5 || got.Batch != 50 || got.DryRun {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
}

func TestRedactErrorHidesRPCURL(t *testing.T) {
	t.Parallel()
	const full = "https://rpc.example/v2/SECRETKEY"
	cause := &url.Error{Op: "Post", URL: full, Err: errors.New("connection refused")}
	err := fmt.Errorf("fetch transaction senders: %w", fmt.Errorf("dial chain RPC: %w", cause))
	got := redactError(err)
	if strings.Contains(got, "SECRETKEY") || strings.Contains(got, full) || !strings.Contains(got, "connection refused") {
		t.Fatalf("redactError = %q", got)
	}
	// A plain-string error (for example from config validation) is covered by the configured URL.
	plain := errors.New("RPC_URL: invalid value " + full + "?k=SECRETKEY")
	if got := redactError(plain, full+"?k=SECRETKEY"); strings.Contains(got, "SECRETKEY") {
		t.Fatalf("redactError = %q", got)
	}
}

func TestParseFlagsRejectsUnusableRate(t *testing.T) {
	t.Parallel()
	for _, arg := range []string{"-rate=Inf", "-rate=NaN", "-rate=+Inf", "-rate=0.0001"} {
		if _, err := parseFlags([]string{arg}); err == nil {
			t.Fatalf("parseFlags(%s) accepted invalid rate", arg)
		}
	}
}
