package http_test

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestNewRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []hqgohttp.ClientOptionFunc
	}{
		{name: "protocol", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithProtocol(hqgohttp.Protocol(99))}},
		{name: "execution timeout", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithTimeout(-time.Second)}},
		{name: "body timeout", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithResponseBodyTimeout(-time.Second)}},
		{name: "attempt timeout", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithAttemptTimeout(-time.Second)}},
		{name: "fallback TTL", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithHTTP3FallbackTTL(-time.Second)}},
		{name: "attempt count", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithRetryMaxAttempts(0)}},
		{name: "read limit", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithResponseReadLimit(0)}},
		{name: "idle threshold", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithMaxRequestsBeforeIdleClose(-1)}},
		{name: "missing HTTP3 client", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithProtocol(hqgohttp.ProtocolHTTP3)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := hqgohttp.New(tt.opts...)
			require.Error(t, err)
			assert.Nil(t, client)
		})
	}
}

func TestNewRejectsTransportConflicts(t *testing.T) {
	t.Parallel()

	custom := &http.Client{}
	proxyURL, err := url.Parse("http://proxy.example")
	require.NoError(t, err)

	tests := []struct {
		name string
		opts []hqgohttp.ClientOptionFunc
	}{
		{name: "custom client and protocol", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithHTTPClient(custom), hqgohttp.WithProtocol(hqgohttp.ProtocolHTTP1)}},
		{name: "two custom clients", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithHTTPClient(custom), hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3), hqgohttp.WithHTTP3Client(custom)}},
		{name: "custom client and TLS", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithHTTPClient(custom), hqgohttp.WithTLSConfig(&tls.Config{})}},
		{name: "custom client and wrapper", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithHTTPClient(custom), hqgohttp.WithTransportWrapper(func(transport http.RoundTripper) http.RoundTripper { return transport })}},
		{name: "custom client and proxy", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithHTTPClient(custom), hqgohttp.WithProxy(func(*http.Request) (*url.URL, error) { return proxyURL, nil })}},
		{name: "HTTP3 and wrapper", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithProtocol(hqgohttp.ProtocolHTTP3), hqgohttp.WithHTTP3Client(custom), hqgohttp.WithTransportWrapper(func(transport http.RoundTripper) http.RoundTripper { return transport })}},
		{name: "unused HTTP3 client", opts: []hqgohttp.ClientOptionFunc{hqgohttp.WithHTTP3Client(custom)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := hqgohttp.New(tt.opts...)
			require.Error(t, err)
			assert.Nil(t, client)
		})
	}
}

func TestNewDefaultsAndValidZeroValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 30*time.Second, hqgohttp.DefaultTimeout)
	assert.Zero(t, hqgohttp.DefaultResponseBodyTimeout)
	assert.Positive(t, hqgohttp.DefaultResponseReadLimit)

	client := mustNew(t,
		hqgohttp.WithHTTPClient(nil),
		hqgohttp.WithTimeout(0),
		hqgohttp.WithResponseBodyTimeout(0),
		hqgohttp.WithAttemptTimeout(0),
		hqgohttp.WithHTTP3FallbackTTL(0),
		hqgohttp.WithRetryPolicy(nil),
		hqgohttp.WithMaxRequestsBeforeIdleClose(0),
	)
	assert.NotNil(t, client)
}
