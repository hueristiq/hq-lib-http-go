package http_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		hqgohttp.ErrNilRequest,
		hqgohttp.ErrNilRequestURL,
		hqgohttp.ErrBodyNotReplayable,
		hqgohttp.ErrHTTP3ClientRequired,
		hqgohttp.ErrHTTP3ProxyUnsupported,
		hqgohttp.ErrRetriesExhausted,
		hqgohttp.ErrClientClosed,
	}
	for _, sentinel := range sentinels {
		require.Error(t, sentinel)
	}

	wrapped := fmt.Errorf("wrapped: %w", hqgohttp.ErrBodyNotReplayable)
	require.ErrorIs(t, wrapped, hqgohttp.ErrBodyNotReplayable)
	require.NotErrorIs(t, wrapped, hqgohttp.ErrRetriesExhausted)
}
