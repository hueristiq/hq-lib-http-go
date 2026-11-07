package http_test

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestClientTransportWrapper(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "wrapped", req.Header.Get("X-Transport"))

		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	var wraps atomic.Int32

	client := mustNew(t, hqgohttp.WithTransportWrapper(func(base http.RoundTripper) http.RoundTripper {
		wraps.Add(1)

		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			clone := req.Clone(req.Context())
			clone.Header = req.Header.Clone()
			clone.Header.Set("X-Transport", "wrapped")

			return base.RoundTrip(clone)
		})
	}))

	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))
	assert.Equal(t, int32(2), wraps.Load(), "primary and fallback transports are wrapped")
}

func TestClientProxy(t *testing.T) {
	t.Parallel()

	var selectors atomic.Int32

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "example.test", req.URL.Host)
		assert.Equal(t, "/resource", req.URL.Path)

		_, _ = io.WriteString(w, "proxied")
	}))
	t.Cleanup(proxy.Close)

	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	client := mustNew(t, hqgohttp.WithProxy(func(*http.Request) (*url.URL, error) {
		selectors.Add(1)

		return proxyURL, nil
	}))

	res, err := client.Do(newRequest(t, http.MethodGet, "http://example.test/resource", http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "proxied", readBody(t, res))
	assert.Positive(t, selectors.Load())
}

func TestClientTLSConfigIsCloned(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "secure")
	}))
	t.Cleanup(server.Close)

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	config := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	client := mustNew(t, hqgohttp.WithTLSConfig(config))
	config.RootCAs = nil

	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "secure", readBody(t, res))
}

func TestClientHTTP3FallbackAndFailureCache(t *testing.T) {
	t.Parallel()

	var h3Calls atomic.Int32

	h3 := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		h3Calls.Add(1)

		return nil, errors.New("QUIC unavailable")
	})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "fallback")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t,
		hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3),
		hqgohttp.WithHTTP3Client(h3),
	)
	for range 2 {
		res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
		require.NoError(t, err)
		assert.Equal(t, "fallback", readBody(t, res))
	}

	assert.Equal(t, int32(1), h3Calls.Load(), "the second request skips a recently failed host")
}

func TestClientHTTP3RejectsProxy(t *testing.T) {
	t.Parallel()

	var h3Calls atomic.Int32

	h3 := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		h3Calls.Add(1)

		return nil, errors.New("unexpected call")
	})}
	proxyURL, err := url.Parse("http://proxy.example")
	require.NoError(t, err)
	client := mustNew(t,
		hqgohttp.WithProtocol(hqgohttp.ProtocolHTTP3),
		hqgohttp.WithHTTP3Client(h3),
		hqgohttp.WithProxy(func(*http.Request) (*url.URL, error) { return proxyURL, nil }),
	)

	res, err := client.Do(newRequest(t, http.MethodGet, "https://example.com", http.NoBody)) //nolint:bodyclose // Proxy rejection returns no response.
	require.ErrorIs(t, err, hqgohttp.ErrHTTP3ProxyUnsupported)
	assert.Nil(t, res)
	assert.Zero(t, h3Calls.Load())
}

func TestClientAutoHTTP3WithoutClientUsesHTTP1Or2(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "auto")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t, hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3))
	res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "auto", readBody(t, res))
}

type closeCountingTransport struct {
	closes atomic.Int32
}

func (t *closeCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
		Request:    req,
	}, nil
}

func (t *closeCountingTransport) CloseIdleConnections() {
	t.closes.Add(1)
}

func TestClientPeriodicIdleConnectionClose(t *testing.T) {
	t.Parallel()

	transport := &closeCountingTransport{}
	client := mustNew(t,
		hqgohttp.WithHTTPClient(&http.Client{Transport: transport}),
		hqgohttp.WithMaxRequestsBeforeIdleClose(1),
	)

	res, err := client.Do(newRequest(t, http.MethodGet, "http://example.com", http.NoBody))
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, int32(1), transport.closes.Load())
}

func TestClientCustomTransport(t *testing.T) {
	t.Parallel()

	transport := &closeCountingTransport{}
	client := mustNew(t, hqgohttp.WithHTTPClient(&http.Client{Transport: transport, Timeout: time.Second}))

	res, err := client.Do(newRequest(t, http.MethodGet, "http://example.com", http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))

	client.CloseIdleConnections()
	assert.Equal(t, int32(1), transport.closes.Load())
}
