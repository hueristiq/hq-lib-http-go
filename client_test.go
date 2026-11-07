package http_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

type roundTripFunc func(req *http.Request) (res *http.Response, err error)

func (f roundTripFunc) RoundTrip(req *http.Request) (res *http.Response, err error) {
	return f(req)
}

func mustNew(tb testing.TB, ofs ...hqgohttp.ClientOptionFunc) (client *hqgohttp.Client) {
	tb.Helper()

	client, err := hqgohttp.New(ofs...)
	require.NoError(tb, err)
	tb.Cleanup(client.Close)

	return client
}

func newRequest(tb testing.TB, method, rawURL string, body io.Reader) (req *http.Request) {
	tb.Helper()

	req, err := http.NewRequestWithContext(tb.Context(), method, rawURL, body)
	require.NoError(tb, err)

	return req
}

func readBody(tb testing.TB, res *http.Response) (body string) {
	tb.Helper()

	require.NotNil(tb, res)

	data, err := io.ReadAll(res.Body)
	require.NoError(tb, err)
	require.NoError(tb, res.Body.Close())

	return string(data)
}

func TestClientDoUsesStandardRequestAndResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "request", req.Header.Get("X-Default"))
		assert.Equal(t, hqgohttp.DefaultUserAgent, req.Header.Get("User-Agent"))
		assert.Equal(t, "default.example", req.Host)

		_, _ = io.WriteString(w, "payload")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t,
		hqgohttp.WithHeader("X-Default", "client"),
		hqgohttp.WithHeader("Host", "default.example"),
	)
	req := newRequest(t, http.MethodGet, server.URL, http.NoBody)
	req.Header.Set("X-Default", "request")

	res, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "payload", readBody(t, res))
	assert.Empty(t, req.Header.Get("User-Agent"))
	assert.Equal(t, req.URL.Host, req.Host)
}

func TestClientDoReturnsFinalHTTPStatus(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "still unavailable")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t, hqgohttp.WithRetryMaxAttempts(2))
	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))

	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	assert.Equal(t, "still unavailable", readBody(t, res))
	assert.Equal(t, int32(2), attempts.Load())
}

func TestClientDoValidation(t *testing.T) {
	t.Parallel()

	client := mustNew(t)

	res, err := client.Do(nil) //nolint:bodyclose // Validation returns no response.
	require.ErrorIs(t, err, hqgohttp.ErrNilRequest)
	assert.Nil(t, res)

	res, err = client.Do(&http.Request{}) //nolint:bodyclose // Validation returns no response.
	require.ErrorIs(t, err, hqgohttp.ErrNilRequestURL)
	assert.Nil(t, res)

	client.Close()

	res, err = client.Do(newRequest(t, http.MethodGet, "http://example.com", http.NoBody)) //nolint:bodyclose // The closed client returns no response.
	require.ErrorIs(t, err, hqgohttp.ErrClientClosed)
	assert.Nil(t, res)
}

func TestClientCloseCancelsInflightRequest(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(started)
		<-req.Context().Done()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := mustNew(t)

	type result struct {
		res *http.Response
		err error
	}

	finished := make(chan result, 1)
	req := newRequest(t, http.MethodGet, server.URL, http.NoBody)

	go func() {
		res, err := client.Do(req) //nolint:bodyclose // Close cancels the request before a response arrives.
		finished <- result{res: res, err: err}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach the server")
	}

	client.Close()

	select {
	case outcome := <-finished:
		require.ErrorIs(t, outcome.err, hqgohttp.ErrClientClosed)
		assert.Nil(t, outcome.res)
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the request")
	}
}

func TestClientRoundTripMatchesDoStatusSemantics(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	client := mustNew(t, hqgohttp.WithRetryMaxAttempts(1))

	res, err := client.RoundTrip(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadGateway, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestClientCustomClientIsCloned(t *testing.T) {
	t.Parallel()

	originalRedirect := func(*http.Request, []*http.Request) error { return nil }
	original := &http.Client{CheckRedirect: originalRedirect, Timeout: time.Second}
	replacementRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	client := mustNew(t,
		hqgohttp.WithHTTPClient(original),
		hqgohttp.WithCheckRedirect(replacementRedirect),
	)

	assert.NotNil(t, original.CheckRedirect)
	assert.Equal(t, time.Second, original.Timeout)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))
}

func TestClientConcurrentRequests(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t)

	const count = 16

	var wg sync.WaitGroup

	for range count {
		wg.Add(1)

		req := newRequest(t, http.MethodGet, server.URL, http.NoBody)

		go func() {
			defer wg.Done()

			res, err := client.Do(req)
			if assert.NoError(t, err) {
				body, readErr := io.ReadAll(res.Body)
				assert.NoError(t, readErr)
				assert.Equal(t, "ok", string(body))
				assert.NoError(t, res.Body.Close())
			}
		}()
	}

	wg.Wait()
}

func TestClientPolicyAbortReturnsResponseAndError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "response")
	}))
	t.Cleanup(server.Close)

	policyErr := errors.New("policy aborted")
	client := mustNew(t, hqgohttp.WithRetryPolicy(func(context.Context, *http.Request, *http.Response, error, int) (bool, error) {
		return false, policyErr
	}))

	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.ErrorIs(t, err, policyErr)
	assert.Equal(t, "response", readBody(t, res))
}

func TestClientContextCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		close(started)
		<-req.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)

	client := mustNew(t)
	finished := make(chan error, 1)

	go func() {
		_, err := client.Do(req) //nolint:bodyclose // Cancellation returns no response.
		finished <- err
	}()

	<-started
	cancel()

	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("request context was not propagated")
	}
}
