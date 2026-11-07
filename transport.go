package http

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"time"
)

func cloneHTTPClient(client *http.Client) (clone *http.Client) {
	if client == nil {
		return nil
	}

	copied := *client

	return &copied
}

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

func (c *Client) configureHTTP3Client() (client *http.Client) {
	client = cloneHTTPClient(c.config.HTTP3Client)

	return c.applyClientOptions(client)
}

func (c *Client) applyProxy(transport http.RoundTripper) (configured http.RoundTripper) {
	if c.config.Proxy == nil {
		return transport
	}

	if httpTransport, ok := transport.(*http.Transport); ok {
		httpTransport.Proxy = c.config.Proxy
	}

	return transport
}

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

func (c *Client) configureTransport(transport http.RoundTripper) (configured http.RoundTripper) {
	transport = c.applyProxy(transport)

	return c.applyTransportWrapper(transport)
}

func (c *Client) proxyFor(req *http.Request) (proxyURL *url.URL, err error) {
	if c.config.Proxy != nil {
		return c.config.Proxy(req)
	}

	return http.ProxyFromEnvironment(req)
}

func (c *Client) buildAutoClient() (client *http.Client) {
	client = newAutoHTTPClient(c.config.TLSConfig)
	client.Transport = c.configureTransport(client.Transport)
	c.applyClientOptions(client)

	return client
}

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

func shouldFallback(fallbackOnHTTP3 bool, err error) (fallback bool) {
	if fallbackOnHTTP3 {
		return isHTTP3FallbackError(err)
	}

	return isHTTP1BrokenTransportError(err)
}

func isHTTP3FallbackError(err error) (fallback bool) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	return !isPermanentTransportError(err)
}

const http3FailureSweepInterval = 64

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

func (c *Client) recordHTTP3Failure(host string) {
	if c.config.HTTP3FallbackTTL <= 0 || host == "" {
		return
	}

	c.http3Failures.Store(host, time.Now())

	if c.http3FailureWrites.Add(1)%http3FailureSweepInterval == 0 {
		c.pruneHTTP3Failures()
	}
}

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

func (c *Client) clearHTTP3Failure(host string) {
	if host != "" {
		c.http3Failures.Delete(host)
	}
}

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
