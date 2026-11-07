package http_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestClientRetriesStandardGetBody(t *testing.T) {
	t.Parallel()

	var (
		attempts atomic.Int32
		bodies   []string
		mu       sync.Mutex
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		assert.NoError(t, err)

		mu.Lock()

		bodies = append(bodies, string(body))
		mu.Unlock()

		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		_, _ = io.WriteString(w, "accepted")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t,
		hqgohttp.WithRetryMaxAttempts(2),
		hqgohttp.WithRetryIdempotent(false),
	)
	req := newRequest(t, http.MethodPost, server.URL, strings.NewReader("payload"))

	res, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "accepted", readBody(t, res))
	assert.Equal(t, int32(2), attempts.Load())
	assert.Equal(t, []string{"payload", "payload"}, bodies)
}

func TestClientDoesNotRetryNonIdempotentMethodByDefault(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	res, err := mustNew(t).Do(newRequest(t, http.MethodPost, server.URL, strings.NewReader("payload")))
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClientNonReplayableHTTPResponseIsReturned(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "last response")
	}))
	t.Cleanup(server.Close)

	body := io.NopCloser(strings.NewReader("payload"))
	req := newRequest(t, http.MethodGet, server.URL, body)
	req.GetBody = nil

	res, err := mustNew(t, hqgohttp.WithRetryMaxAttempts(3)).Do(req)
	require.NoError(t, err)
	assert.Equal(t, "last response", readBody(t, res))
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClientNonReplayableTransportFailure(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("write failed")
	client := mustNew(t,
		hqgohttp.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			_, _ = io.Copy(io.Discard, req.Body)

			return nil, transportErr
		})}),
	)
	req := newRequest(t, http.MethodGet, "http://example.com", io.NopCloser(strings.NewReader("payload")))
	req.GetBody = nil

	res, err := client.Do(req) //nolint:bodyclose // Replay failure returns no response.
	require.ErrorIs(t, err, hqgohttp.ErrBodyNotReplayable)
	require.NotErrorIs(t, err, hqgohttp.ErrRetriesExhausted)
	assert.Nil(t, res)

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, 1, retryErr.Attempts)
}

func TestClientAttemptTimeout(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		attempts.Add(1)

		select {
		case <-time.After(150 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(server.Close)

	client := mustNew(t,
		hqgohttp.WithAttemptTimeout(20*time.Millisecond),
		hqgohttp.WithRetryMaxAttempts(2),
		hqgohttp.WithTimeout(2*time.Second),
	)

	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody)) //nolint:bodyclose // The timeout returns no response.
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, res)
	assert.Equal(t, int32(2), attempts.Load())
}

func TestClientRetryHooks(t *testing.T) {
	t.Parallel()

	var (
		attempts    atomic.Int32
		onRequests  atomic.Int32
		onResponses atomic.Int32
		onRetries   atomic.Int32
		prepares    atomic.Int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		assert.Equal(t, "refreshed", req.Header.Get("X-Signature"))

		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t,
		hqgohttp.WithRetryMaxAttempts(2),
		hqgohttp.WithOnRequest(func(hqgohttp.RequestEvent) { onRequests.Add(1) }),
		hqgohttp.WithOnResponse(func(hqgohttp.ResponseEvent) { onResponses.Add(1) }),
		hqgohttp.WithOnRetry(func(event hqgohttp.RetryEvent) {
			onRetries.Add(1)
			assert.Equal(t, 1, event.Attempt)
		}),
		hqgohttp.WithPrepareRetry(func(_ context.Context, req *http.Request, attempt int) error {
			prepares.Add(1)
			assert.Equal(t, 2, attempt)
			req.Header.Set("X-Signature", "refreshed")

			return nil
		}),
	)

	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))
	assert.Equal(t, int32(2), onRequests.Load())
	assert.Equal(t, int32(2), onResponses.Load())
	assert.Equal(t, int32(1), onRetries.Load())
	assert.Equal(t, int32(1), prepares.Load())
}
