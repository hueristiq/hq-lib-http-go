package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestRateLimiterFunc(t *testing.T) {
	t.Parallel()

	var _ hqgohttp.RateLimiter = hqgohttp.RateLimiterFunc(nil)

	t.Run("delegates with the given context", func(t *testing.T) {
		t.Parallel()

		type contextKey struct{}

		errWait := errors.New("rate limit exceeded")

		var (
			calls    atomic.Int32
			gotValue any
		)

		ctx := context.WithValue(t.Context(), contextKey{}, "marker")

		limiter := hqgohttp.RateLimiterFunc(func(ctx context.Context) error {
			calls.Add(1)

			gotValue = ctx.Value(contextKey{})

			return errWait
		})

		err := limiter.Wait(ctx)

		require.ErrorIs(t, err, errWait)
		assert.Equal(t, "marker", gotValue)
		assert.Equal(t, int32(1), calls.Load(), "Expected exactly one delegation")
	})

	t.Run("stops the client execution with its error", func(t *testing.T) {
		t.Parallel()

		errLimit := errors.New("limiter unavailable")

		var calls atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("unreachable"))
		}))

		t.Cleanup(server.Close)

		client := hqgohttp.New(
			hqgohttp.WithRateLimiter(hqgohttp.RateLimiterFunc(func(context.Context) error {
				calls.Add(1)

				return errLimit
			})),
		)

		res, err := client.Get(server.URL)

		require.ErrorIs(t, err, errLimit)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		assert.Nil(t, res)
		assert.Equal(t, int32(1), calls.Load(),
			"Expected the limiter failure to abort before any retry")
	})
}

func TestRateLimiterFunc_Nil(t *testing.T) {
	t.Parallel()

	var limiter hqgohttp.RateLimiterFunc

	err := limiter.Wait(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil limiter")
}
