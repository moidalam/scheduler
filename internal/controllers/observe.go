package controllers

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/moidalam/scheduler/internal/observability"
)

const tracerName = "github.com/moidalam/scheduler/internal/controllers"

// call is one controller operation under observation: a span for the trace
// and a scope for the metric. It is created by begin and closed by end.
type call struct {
	c     *Controller
	op    string
	ctx   context.Context
	scope *observability.OpScope
	span  trace.Span
}

func (c *Controller) begin(ctx context.Context, op string) *call {
	ctx, span := c.tracer.Start(ctx, "controller."+op)
	return &call{c: c, op: op, ctx: ctx, scope: c.ops.Start(ctx, op), span: span}
}

// end sets the outcome and closes the span and metric. Expected rejections are
// logged here, at Info or Warn by status, with the error code. Unexpected
// failures are logged at Error and marked so the edge does not log them again.
// Database failures are not logged here: the Store logs them at its origin.
func (s *call) end(err error) error {
	defer s.span.End()
	defer s.scope.End()
	if err == nil {
		s.scope.SetResult(observability.ResultSuccess)
		return nil
	}

	var apiErr *Error
	if errors.As(err, &apiErr) {
		s.scope.SetResult(resultForStatus(apiErr.Status))
		s.scope.SetTag("error_code", apiErr.Code)
		s.c.log.Log(s.ctx, observability.LevelForStatus(apiErr.Status), "request rejected",
			slog.String("op", s.op),
			slog.String("error_code", apiErr.Code),
			slog.Int("status", apiErr.Status))
		return err
	}

	s.scope.SetResult(observability.ResultError)
	s.span.RecordError(err)
	s.span.SetStatus(codes.Error, err.Error())
	if observability.IsLogged(err) {
		return err
	}
	s.c.log.ErrorContext(s.ctx, "operation failed", slog.String("op", s.op), slog.Any("error", err))
	return observability.MarkLogged(err)
}

func resultForStatus(status int) observability.Result {
	switch {
	case status == 404:
		return observability.ResultNotFound
	case status == 409:
		return observability.ResultConflict
	case status >= 500:
		return observability.ResultError
	default:
		return observability.ResultRejected
	}
}
