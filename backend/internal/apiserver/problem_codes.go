package apiserver

import (
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

// problemTypePrefix starts the `type` of every problem the API returns; the rest is the stable
// code clients map to a message (docs/specs/2026-11-16-user-error-messages-preflight.md).
const problemTypePrefix = "urn:launchpad:problem:"

func init() {
	// huma builds its own errors (request validation, body parsing, huma.ErrorNNN helpers) with
	// an empty type. Give each one a code from its status so no response is untyped.
	base := huma.NewError
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		err := base(status, msg, errs...)
		if model, ok := err.(*huma.ErrorModel); ok && (model.Type == "" || model.Type == "about:blank") {
			model.Type = problemTypePrefix + problemCodeForStatus(status)
		}
		return err
	}
}

// problemCodeForStatus is the code of an error that has no more specific one.
func problemCodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "authentication_failed"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusNotAcceptable:
		return "not_acceptable"
	case http.StatusConflict:
		return "conflict"
	case http.StatusPreconditionFailed:
		return "revision_conflict"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusUnsupportedMediaType:
		return "unsupported_media_type"
	case http.StatusUnprocessableEntity:
		return "invalid_request"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusInternalServerError:
		return "internal_error"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	case http.StatusGatewayTimeout:
		return "timeout"
	default:
		return "http_" + strconv.Itoa(status)
	}
}
