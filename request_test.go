package http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestClientPerRequestAttemptTimeoutOverride(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(75 * time.Millisecond)

		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t,
		hqgohttp.WithAttemptTimeout(20*time.Millisecond),
		hqgohttp.WithTimeout(time.Second),
	)
	res, err := client.Do(
		newRequest(t, http.MethodGet, server.URL, http.NoBody),
		hqgohttp.WithRequestAttemptTimeout(0),
	)
	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))
}

func TestClientPerRequestRetryPolicy(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	policy := func(context.Context, *http.Request, *http.Response, error, int) (bool, error) {
		return false, nil
	}
	res, err := mustNew(t).Do(
		newRequest(t, http.MethodGet, server.URL, http.NoBody),
		hqgohttp.WithRequestRetryPolicy(policy),
	)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClientPerRequestIdempotencyOverride(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t, hqgohttp.WithRetryMaxAttempts(2))
	res, err := client.Do(
		newRequest(t, http.MethodPost, server.URL, strings.NewReader("payload")),
		hqgohttp.WithRequestRetryIdempotent(false),
	)
	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))
	assert.Equal(t, int32(2), attempts.Load())
}
