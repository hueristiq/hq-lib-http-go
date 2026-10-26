package http

import (
	"net/http"
	"net/url"

	"github.com/quic-go/quic-go/http3"
)

// cloneHTTPClient returns a shallow copy of client. It is used before applying
// library-managed fields so the caller's client is never mutated.
//
// Parameters:
//   - client (*http.Client): the client to copy.
//
// Returns:
//   - clone (*http.Client): the copy, or nil when client is nil.
func cloneHTTPClient(client *http.Client) (clone *http.Client) {
	if client == nil {
		return nil
	}

	copied := *client
	clone = &copied

	return clone
}

// applyClientOptions sets the redirect policy and cookie jar on client when
// the corresponding options were supplied.
//
// Parameters:
//   - client (*http.Client): the client to configure.
//
// Returns:
//   - configured (*http.Client): the same client.
func (c *Client) applyClientOptions(client *http.Client) (configured *http.Client) {
	if client == nil {
		return nil
	}

	if c.checkRedirectSet {
		client.CheckRedirect = c.CheckRedirect
	}

	if c.cookieJarSet {
		client.Jar = c.CookieJar
	}

	configured = client

	return configured
}

// applyProxy sets the proxy selector on transport when the client has one and
// transport is an [http.Transport]. HTTP/3 transports are left unchanged.
//
// Parameters:
//   - transport (http.RoundTripper): the transport to configure.
//
// Returns:
//   - configured (http.RoundTripper): the same transport.
func (c *Client) applyProxy(transport http.RoundTripper) (configured http.RoundTripper) {
	if c.Proxy == nil {
		return transport
	}

	if httpTransport, ok := transport.(*http.Transport); ok {
		httpTransport.Proxy = c.Proxy
	}

	configured = transport

	return configured
}

// applyTransportWrapper wraps transport when the client has a wrapper. A
// wrapper that returns nil leaves the original transport unchanged.
//
// Parameters:
//   - transport (http.RoundTripper): the transport to wrap.
//
// Returns:
//   - wrapped (http.RoundTripper): the wrapped transport.
func (c *Client) applyTransportWrapper(transport http.RoundTripper) (wrapped http.RoundTripper) {
	if c.TransportWrapper == nil {
		return transport
	}

	wrapped = c.TransportWrapper(transport)
	if wrapped == nil {
		wrapped = transport
	}

	return wrapped
}

// configureBuiltTransport applies the dial and TLS settings and the transport
// builder to a library-built transport. Settings are applied only to an
// *http.Transport; HTTP/3 transports are returned unchanged.
//
// Parameters:
//   - protocol (Protocol): the selected protocol, passed to the builder.
//   - transport (http.RoundTripper): the protocol-built transport.
//
// Returns:
//   - configured (http.RoundTripper): the configured transport.
func (c *Client) configureBuiltTransport(protocol Protocol, transport http.RoundTripper) (configured http.RoundTripper) {
	configured = transport

	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		return configured
	}

	if c.DialContext != nil {
		httpTransport.DialContext = c.DialContext
	}

	if c.tlsHandshakeTimeoutSet {
		httpTransport.TLSHandshakeTimeout = c.TLSHandshakeTimeout
	}

	if c.TransportBuilder != nil {
		if built := c.TransportBuilder(protocol, httpTransport); built != nil {
			configured = built
		}
	}

	return configured
}

// configureHTTP3Transport applies the HTTP/3 transport builder to a
// library-built HTTP/3 transport. Other transport types are returned
// unchanged.
//
// Parameters:
//   - transport (http.RoundTripper): the HTTP/3 transport.
//
// Returns:
//   - configured (http.RoundTripper): the configured transport.
func (c *Client) configureHTTP3Transport(transport http.RoundTripper) (configured http.RoundTripper) {
	if c.HTTP3TransportBuilder == nil {
		return transport
	}

	h3Transport, ok := transport.(*http3.Transport)
	if !ok {
		return transport
	}

	if built := c.HTTP3TransportBuilder(h3Transport); built != nil {
		return built
	}

	return h3Transport
}

// configureTransport applies the built-transport settings, the client's proxy,
// and the wrapper to a library-built transport. A supplied transport takes
// precedence over all of them.
//
// Parameters:
//   - protocol (Protocol): the selected protocol, passed to the builder.
//   - transport (http.RoundTripper): the protocol-built transport.
//
// Returns:
//   - configured (http.RoundTripper): the configured transport.
func (c *Client) configureTransport(protocol Protocol, transport http.RoundTripper) (configured http.RoundTripper) {
	if c.Transport != nil {
		return c.Transport
	}

	transport = c.configureBuiltTransport(protocol, transport)
	transport = c.applyProxy(transport)
	configured = c.applyTransportWrapper(transport)

	return configured
}

// proxyFor resolves the proxy that applies to req. It uses the client's
// selector when one was supplied and the environment proxy otherwise. A nil
// result means the request is sent directly.
//
// Parameters:
//   - req (*http.Request): the request to resolve the proxy for.
//
// Returns:
//   - proxyURL (*url.URL): the proxy to use, or nil.
//   - err (error): non-nil when the selector or the environment is invalid.
func (c *Client) proxyFor(req *http.Request) (proxyURL *url.URL, err error) {
	if c.Proxy != nil {
		proxyURL, err = c.Proxy(req)

		return proxyURL, err
	}

	proxyURL, err = http.ProxyFromEnvironment(req)

	return proxyURL, err
}

// buildAutoClient builds the auto-negotiating client used by [ProtocolAuto]
// and as the HTTP/3 fallback.
//
// Returns:
//   - client (*http.Client): the configured client.
func (c *Client) buildAutoClient() (client *http.Client) {
	client = newAutoHTTPClient(c.TLSConfig)
	client.Transport = c.configureTransport(ProtocolAuto, client.Transport)
	c.applyClientOptions(client)

	return client
}

// buildAutoClients builds the primary and h2c fallback clients for
// [ProtocolAuto].
//
// Returns:
//   - primary (*http.Client): the auto-negotiating client.
//   - fallback (*http.Client): the h2c fallback client.
func (c *Client) buildAutoClients() (primary, fallback *http.Client) {
	primary = newAutoHTTPClient(c.TLSConfig)

	if c.Transport != nil {
		primary.Transport = c.Transport
	} else {
		primary.Transport = c.configureBuiltTransport(ProtocolAuto, primary.Transport)
		primary.Transport = c.applyProxy(primary.Transport)
	}

	c.applyClientOptions(primary)

	fallback = newFallbackClient(primary, c.TLSConfig)

	if c.Transport == nil {
		primary.Transport = c.applyTransportWrapper(primary.Transport)
		fallback.Transport = c.applyTransportWrapper(fallback.Transport)
	}

	c.applyClientOptions(fallback)

	return primary, fallback
}
