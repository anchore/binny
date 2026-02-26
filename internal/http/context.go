package http

import (
	"context"

	"github.com/hashicorp/go-retryablehttp"
)

type ctxKey struct{}

// defaultClient is used when none is in context
var defaultClient = retryablehttp.NewClient()

func init() {
	defaultClient.Logger = nil // silence default logger
}

// WithHTTPClient returns a new context with the provided HTTP client attached.
func WithHTTPClient(ctx context.Context, client *retryablehttp.Client) context.Context {
	return context.WithValue(ctx, ctxKey{}, client)
}

// ClientFromContext retrieves the HTTP client from context.
// Falls back to a default retryable client if none is set.
func ClientFromContext(ctx context.Context) *retryablehttp.Client {
	if client, ok := ctx.Value(ctxKey{}).(*retryablehttp.Client); ok && client != nil {
		return client
	}
	return defaultClient
}
