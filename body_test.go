package http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestClientExecutionTimeoutStopsAtHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := mustNew(t, hqgohttp.WithTimeout(20*time.Millisecond))
	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody)) //nolint:bodyclose // The timeout returns no response.
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, res)
}

func TestClientExecutionTimeoutDoesNotBoundBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "first")
		w.(http.Flusher).Flush()
		time.Sleep(100 * time.Millisecond)

		_, _ = io.WriteString(w, " second")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t, hqgohttp.WithTimeout(30*time.Millisecond))
	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "first second", readBody(t, res))
}

func TestClientResponseBodyTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "first")
		w.(http.Flusher).Flush()
		time.Sleep(100 * time.Millisecond)

		_, _ = io.WriteString(w, " second")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t,
		hqgohttp.WithTimeout(time.Second),
		hqgohttp.WithResponseBodyTimeout(20*time.Millisecond),
	)
	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)

	_, err = io.ReadAll(res.Body)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, res.Body.Close())
}
