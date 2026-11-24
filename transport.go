package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"time"
)

// cloneHTTPClient shallow-copies a caller-supplied client so library-applied
// redirect and cookie options never mutate the original.
func cloneHTTPClient(client *http.Client) (clone *http.Client) {
	if client == nil {
		return nil
	}

	copied := *client

	return &copied
}

// applyClientOptions installs the redirect policy and cookie jar the caller
// explicitly set. Options left unset keep the client's own values.
func (c *Client) applyClientOptions(client *http.Client) (configured *http.Client) {
	if client == nil {
		return nil
	}

	if c.config.checkRedirectSet {
		client.CheckRedirect = c.config.CheckRedirect
	}

	if c.config.cookieJarSet {
		client.Jar = c.config.CookieJar
	}

	return client
}

// configureHTTP3Client clones the injected HTTP/3 client and applies the
// redirect and cookie options to the clone.
func (c *Client) configureHTTP3Client() (client *http.Client) {
	client = cloneHTTPClient(c.config.HTTP3Client)

	return c.applyClientOptions(client)
}

// applyProxy installs the configured proxy selector on a library-built
// transport. Non-*http.Transport wrappers pass through untouched.
func (c *Client) applyProxy(transport http.RoundTripper) (configured http.RoundTripper) {
	if c.config.Proxy == nil {
		return transport
	}

	if httpTransport, ok := transport.(*http.Transport); ok {
		httpTransport.Proxy = c.config.Proxy
	}

	return transport
}

// applyTransportWrapper runs the configured transport decorator. A nil
// wrapper — or one returning nil — preserves the input transport.
func (c *Client) applyTransportWrapper(transport http.RoundTripper) (wrapped http.RoundTripper) {
	if c.config.TransportWrapper == nil {
		return transport
	}

	wrapped = c.config.TransportWrapper(transport)
	if wrapped == nil {
		return transport
	}

	return wrapped
}

// configureTransport applies the proxy and the transport wrapper, in that
// order so the wrapper decorates the proxied transport.
func (c *Client) configureTransport(transport http.RoundTripper) (configured http.RoundTripper) {
	transport = c.applyProxy(transport)

	return c.applyTransportWrapper(transport)
}

// proxyFor resolves the proxy for req: the configured selector when present,
// the environment otherwise.
func (c *Client) proxyFor(req *http.Request) (proxyURL *url.URL, err error) {
	if c.config.Proxy != nil {
		return c.config.Proxy(req)
	}

	return http.ProxyFromEnvironment(req)
}

// buildAutoClient builds the standalone auto-mode client used as the
// HTTP/3-primary fallback: pooled HTTP/1.x-HTTP/2 with the transport fully
// configured.
func (c *Client) buildAutoClient() (client *http.Client) {
	client = newAutoHTTPClient(c.config.TLSConfig)
	client.Transport = c.configureTransport(client.Transport)
	c.applyClientOptions(client)

	return client
}

// buildAutoClients builds the primary and fallback pair for [ProtocolAuto]
// and HTTP/3-less [ProtocolAutoHTTP3]. The fallback derives from the primary
// before the wrapper runs, so wrapping cannot stack decorators.
func (c *Client) buildAutoClients() (primary, fallback *http.Client) {
	primary = newAutoHTTPClient(c.config.TLSConfig)
	primary.Transport = c.applyProxy(primary.Transport)
	c.applyClientOptions(primary)

	fallback = newFallbackClient(primary, c.config.TLSConfig)
	primary.Transport = c.applyTransportWrapper(primary.Transport)
	fallback.Transport = c.applyTransportWrapper(fallback.Transport)
	c.applyClientOptions(fallback)

	return primary, fallback
}

// buildHTTPClients materializes the configured protocol mode into the
// client's primary and fallback HTTP clients, applying TLS, proxy, wrapper,
// redirect, and cookie options. An injected client is cloned and paired with
// a derived fallback.
func (c *Client) buildHTTPClients() {
	if c.config.HTTPClient != nil {
		c.config.HTTPClient = c.applyClientOptions(cloneHTTPClient(c.config.HTTPClient))
		c.fallbackHTTPClient = newFallbackClient(c.config.HTTPClient, nil)

		return
	}

	switch c.config.Protocol {
	case ProtocolAuto:
		c.config.HTTPClient, c.fallbackHTTPClient = c.buildAutoClients()
	case ProtocolHTTP1:
		c.config.HTTPClient = newHTTP1Client(c.config.TLSConfig)
		c.config.HTTPClient.Transport = c.configureTransport(c.config.HTTPClient.Transport)
		c.applyClientOptions(c.config.HTTPClient)
	case ProtocolHTTP2:
		c.config.HTTPClient = newHTTP2Client(c.config.TLSConfig)
		c.config.HTTPClient.Transport = c.configureTransport(c.config.HTTPClient.Transport)
		c.applyClientOptions(c.config.HTTPClient)
	case ProtocolHTTP3:
		c.config.HTTPClient = c.configureHTTP3Client()
		c.http3Primary = true
	case ProtocolAutoHTTP3:
		if c.config.HTTP3Client == nil {
			c.config.HTTPClient, c.fallbackHTTPClient = c.buildAutoClients()

			return
		}

		c.config.HTTPClient = c.configureHTTP3Client()
		c.fallbackHTTPClient = c.buildAutoClient()
		c.fallbackOnHTTP3 = true
		c.http3Primary = true
	}
}

// shouldFallback reports whether a failed primary attempt may resend on the
// fallback client: transient HTTP/3 failures in HTTP/3-fallback mode, or the
// broken-HTTP/1.x-transport signature otherwise.
func shouldFallback(fallbackOnHTTP3 bool, err error) (fallback bool) {
	if fallbackOnHTTP3 {
		return isHTTP3FallbackError(err)
	}

	return isHTTP1BrokenTransportError(err)
}

// isHTTP3FallbackError reports whether err is an HTTP/3 failure worth
// resending on HTTP/2 or HTTP/1.1: anything except caller cancellation and
// the permanent failures [isPermanentTransportError] recognizes.
func isHTTP3FallbackError(err error) (fallback bool) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	return !isPermanentTransportError(err)
}

// http3FailureSweepInterval is the number of failure-cache writes between
// sweeps of expired entries.
const http3FailureSweepInterval = 64

// http3RecentlyFailed reports whether host sits in the HTTP/3 failure cache.
// An expired entry is evicted on lookup and counts as absent. The cache is
// disabled when [WithHTTP3FallbackTTL] is zero.
func (c *Client) http3RecentlyFailed(host string) (failed bool) {
	if c.config.HTTP3FallbackTTL <= 0 || host == "" {
		return false
	}

	value, ok := c.http3Failures.Load(host)
	if !ok {
		return false
	}

	failedAt, ok := value.(time.Time)
	if !ok || time.Since(failedAt) >= c.config.HTTP3FallbackTTL {
		c.http3Failures.Delete(host)

		return false
	}

	return true
}

// recordHTTP3Failure stamps host into the HTTP/3 failure cache, sweeping
// expired entries every http3FailureSweepInterval writes.
func (c *Client) recordHTTP3Failure(host string) {
	if c.config.HTTP3FallbackTTL <= 0 || host == "" {
		return
	}

	c.http3Failures.Store(host, time.Now())

	if c.http3FailureWrites.Add(1)%http3FailureSweepInterval == 0 {
		c.pruneHTTP3Failures()
	}
}

// pruneHTTP3Failures evicts every expired entry from the HTTP/3 failure
// cache.
func (c *Client) pruneHTTP3Failures() {
	ttl := c.config.HTTP3FallbackTTL
	if ttl <= 0 {
		return
	}

	c.http3Failures.Range(func(key, value any) bool {
		failedAt, ok := value.(time.Time)
		if !ok || time.Since(failedAt) >= ttl {
			c.http3Failures.Delete(key)
		}

		return true
	})
}

// clearHTTP3Failure drops host from the HTTP/3 failure cache after a
// successful HTTP/3 exchange.
func (c *Client) clearHTTP3Failure(host string) {
	if host != "" {
		c.http3Failures.Delete(host)
	}
}

// sameRoundTripper reports whether a and b are the same transport value, so
// [Client.CloseIdleConnections] does not close a shared pool twice.
// Incomparable implementations (a transport wrapping a map, say) never match.
func sameRoundTripper(a, b http.RoundTripper) (same bool) {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	aValue := reflect.ValueOf(a)
	bValue := reflect.ValueOf(b)

	if aValue.Type() != bValue.Type() || !aValue.Type().Comparable() {
		return false
	}

	return aValue.Interface() == bValue.Interface()
}
