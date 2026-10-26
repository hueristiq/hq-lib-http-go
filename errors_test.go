package http_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		hqgohttp.ErrNilRequest,
		hqgohttp.ErrNilRequestURL,
		hqgohttp.ErrNoURL,
		hqgohttp.ErrBodyNotReplayable,
		hqgohttp.ErrRequestBodyTooLarge,
		hqgohttp.ErrResponseBodyTooLarge,
		hqgohttp.ErrHTTP3ProxyUnsupported,
		hqgohttp.ErrRetriesExhausted,
		hqgohttp.ErrClientClosed,
	}

	for _, sentinel := range sentinels {
		require.Error(t, sentinel)
	}

	wrapped := fmt.Errorf("wrapped: %w", hqgohttp.ErrRequestBodyTooLarge)

	require.ErrorIs(t, wrapped, hqgohttprequest.ErrBodyTooLarge)
	require.ErrorIs(t, wrapped, hqgohttp.ErrRequestBodyTooLarge)
	require.NotErrorIs(t, wrapped, hqgohttp.ErrResponseBodyTooLarge)
}
