package http

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_WithDialContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "dialed")
	}))

	t.Cleanup(server.Close)

	var called atomic.Bool

	dialer := &net.Dialer{}

	client := New(
		WithDialContext(func(ctx context.Context, network, address string) (net.Conn, error) {
			called.Store(true)

			return dialer.DialContext(ctx, network, address)
		}),
		WithRetryMax(0),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dialed", readBody(t, res))
	assert.True(t, called.Load(), "Expected the custom dial function to be used")

	fallback, ok := client.fallbackHTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.NotNil(t, fallback.DialContext, "Expected the fallback to inherit the dial function")
}

func TestClient_WithDialer(t *testing.T) {
	t.Parallel()

	client := New(WithDialer(&net.Dialer{Timeout: time.Second}))

	transport, ok := client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.NotNil(t, transport.DialContext)

	client = New(WithDialer(nil))

	transport, ok = client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.NotNil(t, transport.DialContext, "Expected the default dialer to remain")
}

func TestClient_WithTLSHandshakeTimeout(t *testing.T) {
	t.Parallel()

	client := New(WithTLSHandshakeTimeout(2 * time.Second))

	transport, ok := client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.Equal(t, 2*time.Second, transport.TLSHandshakeTimeout)

	fallback, ok := client.fallbackHTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.Equal(t, 2*time.Second, fallback.TLSHandshakeTimeout,
		"Expected the fallback to inherit the handshake timeout")

	client = New(WithTLSHandshakeTimeout(0))

	transport, ok = client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.Zero(t, transport.TLSHandshakeTimeout)

	client = New()

	transport, ok = client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.Equal(t, 10*time.Second, transport.TLSHandshakeTimeout,
		"Expected the built transport's default handshake timeout")
}

func TestClient_WithTransportBuilder(t *testing.T) {
	t.Parallel()

	var gotProtocol Protocol

	builder := func(protocol Protocol, transport *http.Transport) *http.Transport {
		gotProtocol = protocol
		transport.MaxIdleConnsPerHost = 7

		return transport
	}

	client := New(WithTransportBuilder(builder))

	assert.Equal(t, ProtocolAuto, gotProtocol)

	primary, ok := client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.Equal(t, 7, primary.MaxIdleConnsPerHost)

	fallback, ok := client.fallbackHTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.Equal(t, 7, fallback.MaxIdleConnsPerHost,
		"Expected the fallback to inherit the built transport")
}

func TestClient_WithTransportBuilderNilResultKeepsTransport(t *testing.T) {
	t.Parallel()

	var original *http.Transport

	client := New(WithTransportBuilder(func(_ Protocol, transport *http.Transport) *http.Transport {
		original = transport

		return nil
	}))

	configured, ok := client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)
	assert.Same(t, original, configured)
}

func TestClient_WithTransportBuilderIgnoredWithTransport(t *testing.T) {
	t.Parallel()

	var called atomic.Bool

	custom := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return newOKResponse("custom"), nil
	})

	client := New(
		WithTransport(custom),
		WithTransportBuilder(func(_ Protocol, transport *http.Transport) *http.Transport {
			called.Store(true)

			return transport
		}),
	)

	require.False(t, called.Load())
	assert.NotNil(t, client.HTTPClient.Transport)
}

func TestClient_WithTransportBuilderSkipsHTTP3(t *testing.T) {
	t.Parallel()

	var called atomic.Bool

	New(
		WithProtocol(ProtocolHTTP3),
		WithTransportBuilder(func(_ Protocol, transport *http.Transport) *http.Transport {
			called.Store(true)

			return transport
		}),
	)

	assert.False(t, called.Load(), "Expected the HTTP/3 transport not to reach the builder")
}

func TestClient_WithCookieJarClonesCustomClient(t *testing.T) {
	t.Parallel()

	jar, err := cookiejar.New(nil)

	require.NoError(t, err)

	custom := &http.Client{}

	client := New(
		WithHTTPClient(custom),
		WithCookieJar(jar),
	)

	assert.NotSame(t, custom, client.HTTPClient)
	assert.Same(t, jar, client.HTTPClient.Jar)
	require.NotNil(t, client.fallbackHTTPClient)
	assert.Same(t, jar, client.fallbackHTTPClient.Jar)
}

func TestClient_WithCheckRedirectClonesCustomClient(t *testing.T) {
	t.Parallel()

	custom := &http.Client{}

	client := New(
		WithHTTPClient(custom),
		WithCheckRedirect(func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}),
	)

	assert.NotSame(t, custom, client.HTTPClient)
	require.NotNil(t, client.HTTPClient.CheckRedirect)
	require.ErrorIs(t, client.HTTPClient.CheckRedirect(&http.Request{}, nil), http.ErrUseLastResponse)
	require.NotNil(t, client.fallbackHTTPClient)
	require.NotNil(t, client.fallbackHTTPClient.CheckRedirect)
	require.ErrorIs(t, client.fallbackHTTPClient.CheckRedirect(&http.Request{}, nil), http.ErrUseLastResponse)
}

func TestClient_TransportPrecedence(t *testing.T) {
	t.Parallel()

	var proxyCalled, wrapperCalled atomic.Bool

	custom := &http.Transport{}

	client := New(
		WithTransport(custom),
		WithProxy(func(_ *http.Request) (*url.URL, error) {
			proxyCalled.Store(true)

			return &url.URL{Scheme: "http", Host: "proxy.example"}, nil
		}),
		WithProtocol(ProtocolHTTP3),
		WithTransportWrapper(func(transport http.RoundTripper) http.RoundTripper {
			wrapperCalled.Store(true)

			return transport
		}),
	)

	assert.Same(t, custom, client.HTTPClient.Transport)
	assert.False(t, proxyCalled.Load(), "Expected WithTransport to bypass WithProxy")
	assert.False(t, wrapperCalled.Load(), "Expected WithTransport to bypass WithTransportWrapper")
}

func TestClient_WithProxyDisabled(t *testing.T) {
	t.Parallel()

	client := New(withoutProxy())

	transport, ok := client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)

	proxy, err := transport.Proxy(&http.Request{})

	require.NoError(t, err)
	assert.Nil(t, proxy, "Expected a nil proxy to disable environment proxying")
}

func TestClient_WithProxy(t *testing.T) {
	t.Parallel()

	proxyURL := &url.URL{Scheme: "http", Host: "proxy.example"}

	var called bool

	client := New(WithProxy(func(_ *http.Request) (*url.URL, error) {
		called = true

		return proxyURL, nil
	}))

	transport, ok := client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)

	proxy, err := transport.Proxy(&http.Request{})

	require.NoError(t, err)
	assert.Equal(t, proxyURL, proxy)
	assert.True(t, called)
}

func TestClient_WithProxySelector(t *testing.T) {
	t.Parallel()

	proxyURL := &url.URL{Scheme: "http", Host: "proxy.example"}

	client := New(fixedProxy(proxyURL))

	transport, ok := client.HTTPClient.Transport.(*http.Transport)

	require.True(t, ok)

	proxy, err := transport.Proxy(&http.Request{})

	require.NoError(t, err)
	assert.Equal(t, proxyURL, proxy)
}

func TestClient_WithTransportWrapperNilFallsBack(t *testing.T) {
	t.Parallel()

	client := New(WithTransportWrapper(func(http.RoundTripper) http.RoundTripper {
		return nil
	}))

	require.NotNil(t, client.HTTPClient.Transport)
}
