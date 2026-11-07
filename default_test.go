package http_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestDefaultTransports(t *testing.T) {
	t.Parallel()

	pooled := hqgohttp.DefaultHTTPPooledTransport()
	require.NotNil(t, pooled)
	assert.True(t, pooled.ForceAttemptHTTP2)
	assert.False(t, pooled.DisableKeepAlives)
	assert.Positive(t, pooled.IdleConnTimeout)
	assert.Positive(t, pooled.TLSHandshakeTimeout)

	transient := hqgohttp.DefaultHTTPTransport()
	assert.True(t, transient.DisableKeepAlives)
	assert.NotSame(t, pooled, transient)

	pooledClient := hqgohttp.DefaultHTTPPooledClient()
	require.NotNil(t, pooledClient)
	assert.NotNil(t, pooledClient.Transport)

	transientClient := hqgohttp.DefaultHTTPClient()
	require.NotNil(t, transientClient)
	assert.NotNil(t, transientClient.Transport)
}
