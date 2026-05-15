package githubrelease

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRetryableGitHubClient_RetriesWithAuthHeader(t *testing.T) {
	var requests int
	var authHeaders []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))

		if requests < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	token := "test-token-12345"
	client := newRetryableGitHubClient(context.Background(), token)

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 3, requests, "expected 3 requests (2 retries)")
	expectedAuth := "Bearer " + token
	for i, auth := range authHeaders {
		assert.Equal(t, expectedAuth, auth, "request %d missing auth header", i+1)
	}
}

func TestGithubRetryPolicy(t *testing.T) {
	// the policy is forward-compat insurance: the current default already declines 403,
	// but pinning it explicitly protects against an upstream change reintroducing
	// wasted backoff on auth failures / secondary rate limits.
	tests := []struct {
		name       string
		statusCode int
		err        error
		wantRetry  bool
	}{
		{name: "200 OK is not retried", statusCode: http.StatusOK, wantRetry: false},
		{name: "403 Forbidden is not retried (pinned)", statusCode: http.StatusForbidden, wantRetry: false},
		{name: "404 Not Found is not retried", statusCode: http.StatusNotFound, wantRetry: false},
		{name: "429 Too Many Requests is retried", statusCode: http.StatusTooManyRequests, wantRetry: true},
		{name: "500 Internal Server Error is retried", statusCode: http.StatusInternalServerError, wantRetry: true},
		{name: "502 Bad Gateway is retried", statusCode: http.StatusBadGateway, wantRetry: true},
		{name: "503 Service Unavailable is retried", statusCode: http.StatusServiceUnavailable, wantRetry: true},
		{name: "504 Gateway Timeout is retried", statusCode: http.StatusGatewayTimeout, wantRetry: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.statusCode}
			gotRetry, err := githubRetryPolicy(context.Background(), resp, tt.err)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRetry, gotRetry)
		})
	}

	t.Run("transport error is retried (delegates to default policy)", func(t *testing.T) {
		gotRetry, _ := githubRetryPolicy(context.Background(), nil, errors.New("connection refused"))
		assert.True(t, gotRetry)
	})
}
