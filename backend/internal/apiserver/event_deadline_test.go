package apiserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The middleware clears the server-wide write deadline for /v1/events; a stalled reader is still
// cut off because huma's sse.Sender sets a per-message write deadline (sse.WriteTimeout) on the
// first writer in the Unwrap chain that supports it. This pins that the chain reaches one.
func TestEventStreamWriterSupportsPerMessageWriteDeadline(t *testing.T) {
	type deadliner interface{ SetWriteDeadline(time.Time) error }
	type unwrapper interface{ Unwrap() http.ResponseWriter }
	result := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, _ *http.Request) {
		var current any = w
		for {
			if d, ok := current.(deadliner); ok {
				result <- d.SetWriteDeadline(time.Now().Add(time.Second))
				return
			}
			u, ok := current.(unwrapper)
			if !ok {
				result <- http.ErrNotSupported
				return
			}
			current = u.Unwrap()
		}
	})
	server := httptest.NewServer(middleware(mux, DefaultConfig(), NewRateLimiter(600, 120), slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	resp, err := http.Get(server.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if err := <-result; err != nil {
		t.Fatalf("stream writer has no usable write deadline: %v", err)
	}
}
