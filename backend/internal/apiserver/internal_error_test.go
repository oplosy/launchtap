package apiserver

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestMapReadErrorLogsTheCauseOfA500(t *testing.T) {
	var buffer bytes.Buffer
	ctx := context.WithValue(context.Background(), loggerKey, slog.New(slog.NewTextHandler(&buffer, nil)))
	ctx = context.WithValue(ctx, requestIDKey, "req-1")
	err := mapReadError(ctx, errors.New("relation missing at https://user:secret@db.example/x"))
	var problem *huma.ErrorModel
	if !errors.As(err, &problem) || problem.Status != 500 || strings.Contains(problem.Detail, "relation") {
		t.Fatalf("problem = %#v, want a generic 500", err)
	}
	logged := buffer.String()
	for _, want := range []string{"request failed", "request_id=req-1", "relation missing"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log %q lacks %q", logged, want)
		}
	}
	if strings.Contains(logged, "secret") {
		t.Fatalf("log %q leaks a URL credential", logged)
	}
}

func TestMapReadErrorDoesNotLogClientErrors(t *testing.T) {
	var buffer bytes.Buffer
	ctx := context.WithValue(context.Background(), loggerKey, slog.New(slog.NewTextHandler(&buffer, nil)))
	_ = mapReadError(ctx, context.Canceled)
	if buffer.Len() != 0 {
		t.Fatalf("canceled request logged %q", buffer.String())
	}
}
