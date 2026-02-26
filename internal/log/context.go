package log

import (
	"context"

	"github.com/anchore/go-logger"
)

type ctxKey struct{}

// WithLogger returns a new context with the provided logger attached.
func WithLogger(ctx context.Context, lgr logger.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, lgr)
}

// FromContext retrieves the logger from context. Falls back to global logger.
func FromContext(ctx context.Context) logger.Logger {
	if lgr, ok := ctx.Value(ctxKey{}).(logger.Logger); ok && lgr != nil {
		return lgr
	}
	return Get()
}

// WithNested gets the logger from context, creates a nested logger with the provided
// key-value fields, and returns both the new context and the nested logger.
func WithNested(ctx context.Context, fields ...any) (context.Context, logger.Logger) {
	lgr := FromContext(ctx).Nested(fields...)
	return WithLogger(ctx, lgr), lgr
}
