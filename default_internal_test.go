package http

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFallbackTransport(t *testing.T) {
	t.Parallel()

	defaultTransport, ok := newFallbackTransport(nil, nil).(*http.Transport)

	require.True(t, ok)
	assert.True(t, defaultTransport.Protocols.UnencryptedHTTP2())
	assert.True(t, defaultTransport.DisableKeepAlives)

	proxyURL := &url.URL{Scheme: "http", Host: "proxy.example"}
	base := &http.Transport{Proxy: http.ProxyURL(proxyURL)}

	fallback, ok := newFallbackTransport(base, nil).(*http.Transport)

	require.True(t, ok)
	assert.NotSame(t, base, fallback)
	assert.True(t, fallback.Protocols.UnencryptedHTTP2())

	proxy, err := fallback.Proxy(&http.Request{})

	require.NoError(t, err)
	assert.Equal(t, proxyURL, proxy)
}
