package http_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestNewRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []hqgohttp.ClientOptionFunc
	}{
		{
			name: "protocol out of range",
			opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithProtocol(hqgohttp.Protocol(99))},
		},
		{
			name: "negative timeout",
			opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithTimeout(-time.Second)},
		},
		{
			name: "negative response body timeout",
			opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithResponseBodyTimeout(-time.Second)},
		},
		{
			name: "negative response body size",
			opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithMaxResponseBodySize(-1)},
		},
		{
			name: "negative request body size",
			opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithMaxRequestBodySize(-1)},
		},
		{
			name: "zero retry attempts",
			opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithRetryMaxAttempts(0)},
		},
		{
			name: "zero response read limit",
			opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithResponseReadLimit(0)},
		},
		{
			name: "negative retry error body limit",
			opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithRetryErrorBodyLimit(-1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := hqgohttp.New(tt.opts...)
			require.Error(t, err)
		})
	}
}

func TestNewAcceptsDefaults(t *testing.T) {
	t.Parallel()

	client, err := hqgohttp.New()
	require.NoError(t, err)
	require.NotNil(t, client)

	client.Close()
}
