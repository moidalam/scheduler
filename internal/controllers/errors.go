package controllers

import "net/http"

// Error is an API-facing failure. The handler layer renders it as RFC 9457
// problem+json; Code is stable and machine-readable (ADR-0006).
type Error struct {
	Status int
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

// Title is the short human-readable summary of the HTTP status.
func (e *Error) Title() string { return http.StatusText(e.Status) }

func badRequest(code, detail string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: code, Detail: detail}
}

func notFound(code, detail string) *Error {
	return &Error{Status: http.StatusNotFound, Code: code, Detail: detail}
}

func conflict(code, detail string) *Error {
	return &Error{Status: http.StatusConflict, Code: code, Detail: detail}
}

func unprocessable(code, detail string) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: code, Detail: detail}
}
