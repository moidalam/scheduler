package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/moidalam/scheduler/internal/controllers"
	"github.com/moidalam/scheduler/internal/gen/api"
	"github.com/moidalam/scheduler/internal/observability"
)

const problemContentType = "application/problem+json"

// writeProblem renders an RFC 9457 problem document (ADR-0006). Clients branch
// on code; detail is for humans and may change.
func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	p := api.Problem{
		Title:  http.StatusText(status),
		Status: status,
		Code:   code,
		Detail: &detail,
	}
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// responseErrorHandler turns an error returned by a handler into a response.
// Known application errors keep their status and code; anything else is logged
// in full and reported to the caller as a generic 500, so internals never leak.
func responseErrorHandler(log *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		var apiErr *controllers.Error
		if errors.As(err, &apiErr) {
			// The controller already logged this at its origin. Only the code
			// is recorded here, for the edge line.
			setErrorCode(r, apiErr.Code)
			writeProblem(w, apiErr.Status, apiErr.Code, apiErr.Detail)
			return
		}
		setErrorCode(r, "internal_error")
		if !observability.IsLogged(err) {
			// Nothing below logged it, so this is the origin for the failure.
			log.ErrorContext(r.Context(), "unhandled error", slog.Any("error", err),
				slog.String("method", r.Method), slog.String("path", r.URL.Path))
		}
		writeProblem(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
	}
}

// setErrorCode records the bounded error code on the request record, where the
// edge reads it for its log line and metric tag.
func setErrorCode(r *http.Request, code string) {
	if rec := observability.RecordFrom(r.Context()); rec != nil {
		rec.ErrorCode = code
	}
}

// requestErrorHandler renders a request the generated code could not parse:
// a bad path or query parameter, or an undecodable body.
func requestErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	setErrorCode(r, "invalid_request")
	writeProblem(w, http.StatusBadRequest, "invalid_request", err.Error())
}
