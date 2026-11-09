package redact

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestErrorHidesRPCURL(t *testing.T) {
	t.Parallel()
	const full = "https://rpc.example/v2/SECRETKEY"
	cause := &url.Error{Op: "Post", URL: full, Err: errors.New("connection refused")}
	err := fmt.Errorf("fetch transaction senders: %w", fmt.Errorf("dial chain RPC: %w", cause))
	got := Error(err)
	if strings.Contains(got, "SECRETKEY") || strings.Contains(got, full) || !strings.Contains(got, "connection refused") {
		t.Fatalf("Error = %q", got)
	}
	// A plain-string error (for example from config validation) is covered by the configured URL.
	plain := errors.New("RPC_URL: invalid value " + full + "?k=SECRETKEY")
	if got := Error(plain, full+"?k=SECRETKEY"); strings.Contains(got, "SECRETKEY") {
		t.Fatalf("Error = %q", got)
	}
	// Any other URL-looking text is swept too.
	if got := Error(errors.New(`Post "wss://node.example/ws?key=SECRETKEY": EOF`)); strings.Contains(got, "SECRETKEY") || !strings.Contains(got, Placeholder) {
		t.Fatalf("Error = %q", got)
	}
	if got := Error(nil); got != "" {
		t.Fatalf("Error(nil) = %q", got)
	}
}
