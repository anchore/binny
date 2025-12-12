package githubrelease

import (
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
	client := newRetryableGitHubClient(token)

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 3, requests, "expected 3 requests (2 retries)")
	expectedAuth := "Bearer " + token
	for i, auth := range authHeaders {
		assert.Equal(t, expectedAuth, auth, "request %d missing auth header", i+1)
	}
}
