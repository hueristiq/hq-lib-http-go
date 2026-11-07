package http_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestRateLimiterFunc(t *testing.T) {
	t.Parallel()

	var _ hqgohttp.RateLimiter = hqgohttp.RateLimiterFunc(nil)

	t.Run("receives context and request", func(t *testing.T) {
		t.Parallel()

		type contextKey struct{}

		errWait := errors.New("rate limit exceeded")

		var (
			calls      atomic.Int32
			gotValue   any
			gotRequest *http.Request
		)

		ctx := context.WithValue(t.Context(), contextKey{}, "marker")
		req := newRequest(t, http.MethodGet, "http://example.com", http.NoBody)
		limiter := hqgohttp.RateLimiterFunc(func(ctx context.Context, req *http.Request) error {
			calls.Add(1)

			gotValue = ctx.Value(contextKey{})
			gotRequest = req

			return errWait
		})

		err := limiter.Wait(ctx, req)
		require.ErrorIs(t, err, errWait)
		assert.Equal(t, "marker", gotValue)
		assert.Same(t, req, gotRequest)
		assert.Equal(t, int32(1), calls.Load())
	})

	t.Run("stops the client before an attempt", func(t *testing.T) {
		t.Parallel()

		errLimit := errors.New("limiter unavailable")

		var calls atomic.Int32

		client := mustNew(t, hqgohttp.WithRateLimiter(hqgohttp.RateLimiterFunc(func(context.Context, *http.Request) error {
			calls.Add(1)

			return errLimit
		})))

		res, err := client.Do(newRequest(t, http.MethodGet, "http://example.com", http.NoBody)) //nolint:bodyclose // The limiter rejects the request before transport.
		require.ErrorIs(t, err, errLimit)
		assert.Nil(t, res)
		assert.Equal(t, int32(1), calls.Load())
	})
}

func TestRateLimiterFuncNil(t *testing.T) {
	t.Parallel()

	var limiter hqgohttp.RateLimiterFunc

	err := limiter.Wait(t.Context(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil limiter")
}

func TestClientRequestAwareRateLimiter(t *testing.T) {
	t.Parallel()

	var limitedHost atomic.Value

	client := mustNew(t,
		hqgohttp.WithRateLimiter(hqgohttp.RateLimiterFunc(func(_ context.Context, req *http.Request) error {
			limitedHost.Store(req.URL.Hostname())

			return nil
		})),
		hqgohttp.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    req,
			}, nil
		})}),
	)

	res, err := client.Do(newRequest(t, http.MethodGet, "http://example.com/resource", http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))
	assert.Equal(t, "example.com", limitedHost.Load())
}
