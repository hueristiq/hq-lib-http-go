package http_test

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestStripSensitiveHeadersOnRedirectHostChange(t *testing.T) {
	t.Parallel()

	var received atomic.Value

	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received.Store(req.Header.Get("X-Api-Key"))

		_, _ = io.WriteString(w, "destination")
	}))
	t.Cleanup(destination.Close)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, destination.URL, http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	client := mustNew(t, hqgohttp.WithCheckRedirect(hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key")))
	req := newRequest(t, http.MethodGet, origin.URL, http.NoBody)
	req.Header.Set("X-Api-Key", "secret")

	res, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "destination", readBody(t, res))
	assert.Empty(t, received.Load())
}

func TestStripSensitiveHeadersOnRedirectSameHost(t *testing.T) {
	t.Parallel()

	var received atomic.Value

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/redirect" {
			http.Redirect(w, req, "/destination", http.StatusFound)

			return
		}

		received.Store(req.Header.Get("X-Api-Key"))

		_, _ = io.WriteString(w, "destination")
	}))
	t.Cleanup(server.Close)

	client := mustNew(t, hqgohttp.WithCheckRedirect(hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key")))
	req := newRequest(t, http.MethodGet, server.URL+"/redirect", http.NoBody)
	req.Header.Set("X-Api-Key", "secret")

	res, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "destination", readBody(t, res))
	assert.Equal(t, "secret", received.Load())
}

func TestStripSensitiveHeadersOnRedirectDowngrade(t *testing.T) {
	t.Parallel()

	var received atomic.Value

	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received.Store(req.Header.Get("X-Api-Key"))

		_, _ = io.WriteString(w, "plain")
	}))
	t.Cleanup(destination.Close)

	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, destination.URL, http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	client := mustNew(t,
		hqgohttp.WithTLSConfig(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}),
		hqgohttp.WithCheckRedirect(hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key")),
	)
	req := newRequest(t, http.MethodGet, origin.URL, http.NoBody)
	req.Header.Set("X-Api-Key", "secret")

	res, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "plain", readBody(t, res))
	assert.Empty(t, received.Load())
}

func TestStripSensitiveHeadersOnRedirectMultiHop(t *testing.T) {
	t.Parallel()

	var (
		received      atomic.Value
		origin, other *httptest.Server
	)

	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/start" {
			http.Redirect(w, req, other.URL, http.StatusFound)

			return
		}

		received.Store(req.Header.Get("X-Api-Key"))

		_, _ = io.WriteString(w, "final")
	}))
	t.Cleanup(origin.Close)

	other = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, origin.URL+"/final", http.StatusFound)
	}))
	t.Cleanup(other.Close)

	client := mustNew(t, hqgohttp.WithCheckRedirect(hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key", "")))
	req := newRequest(t, http.MethodGet, origin.URL+"/start", http.NoBody)
	req.Header.Set("X-Api-Key", "secret")

	res, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, "final", readBody(t, res))
	assert.Empty(t, received.Load())
}
