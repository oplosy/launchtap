package apiserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Contictus/launchtap/backend/internal/redact"
)

const loggerKey ctxKey = "logger"

// internalProblem logs the cause of a 500 with the request id, then returns the generic problem.
// The client never sees the cause; the access log alone records only the status.
func internalProblem(ctx context.Context, err error, code, detail string) error {
	logger, _ := ctx.Value(loggerKey).(*slog.Logger)
	if logger == nil {
		logger = slog.Default()
	}
	logger.ErrorContext(ctx, "request failed", "request_id", RequestID(ctx), "error_type", fmt.Sprintf("%T", err), "error", redact.Error(err))
	return apiRequestProblem(ctx, http.StatusInternalServerError, code, detail)
}
