package http

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/quic-go/quic-go/http3"

	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
)

// defaultClient is the shared client behind the package-level shortcuts,
// built lazily on first use. [New] has no failure mode, so the value is built
// once and reused.
var defaultClient = sync.OnceValue(func() (client *Client) {
	client = New()

	return client
})

// newAutoHTTPClient builds the primary client for [ProtocolAuto] and the
// fallback client for [ProtocolAutoHTTP3]. It negotiates HTTP/2 over TLS and
// falls back to HTTP/1.1.
var newAutoHTTPClient = func(config *tls.Config) (client *http.Client) {
	transport := DefaultHTTPPooledTransport()

	transport.TLSClientConfig = cloneTLSConfig(config)

	client = &http.Client{
		Transport: transport,
	}

	return client
}

// newFallbackClient builds the fallback client used when a cleartext HTTP/1.x
// exchange fails because the server answered with HTTP/2 frames. It clones the
// primary client so CheckRedirect, Jar, Timeout, and future client fields
// survive, then swaps in an h2c-capable transport. It is declared as a
// package-level variable so tests can substitute a client with a scripted
// transport.
var newFallbackClient = func(base *http.Client, config *tls.Config) (client *http.Client) {
	if base == nil {
		base = &http.Client{}
	}

	clone := *base

	clone.Transport = newFallbackTransport(base.Transport, config)

	client = &clone

	return client
}

// DefaultHTTPClient returns a new *http.Client on a [DefaultHTTPTransport],
// so no connection is ever reused.
//
// Returns:
//   - client (*http.Client): a newly created non-pooled client.
func DefaultHTTPClient() (client *http.Client) {
	client = &http.Client{
		Transport: DefaultHTTPTransport(),
	}

	return client
}

// DefaultHTTPPooledClient returns a new *http.Client on a
// [DefaultHTTPPooledTransport].
//
// Returns:
//   - client (*http.Client): a newly created pooled client.
func DefaultHTTPPooledClient() (client *http.Client) {
	client = &http.Client{
		Transport: DefaultHTTPPooledTransport(),
	}

	return client
}

// newHTTP1Client builds a client that speaks only HTTP/1.x.
//
// Parameters:
//   - config (*tls.Config): the TLS configuration to clone; nil uses the
//     system defaults.
//
// Returns:
//   - client (*http.Client): the configured client.
func newHTTP1Client(config *tls.Config) (client *http.Client) {
	transport := DefaultHTTPPooledTransport()

	transport.ForceAttemptHTTP2 = false
	transport.Protocols = newProtocols(true, false, false)
	transport.TLSClientConfig = cloneTLSConfig(config)

	client = &http.Client{
		Transport: transport,
	}

	return client
}

// newHTTP2Client builds a client that speaks only HTTP/2, including h2c
// prior knowledge for http:// URLs.
//
// Parameters:
//   - config (*tls.Config): the TLS configuration to clone; nil uses the
//     system defaults.
//
// Returns:
//   - client (*http.Client): the configured client.
func newHTTP2Client(config *tls.Config) (client *http.Client) {
	transport := DefaultHTTPPooledTransport()

	transport.ForceAttemptHTTP2 = true
	transport.Protocols = newProtocols(false, true, true)
	transport.TLSClientConfig = cloneTLSConfig(config)

	client = &http.Client{
		Transport: transport,
	}

	return client
}

// newHTTP3Client builds a client that speaks only HTTP/3. It is declared as a
// package-level variable so tests can substitute a client with a scripted
// transport.
var newHTTP3Client = func(config *tls.Config) (client *http.Client) {
	client = &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: cloneTLSConfig(config),
		},
	}

	return client
}

// newFallbackTransport builds a transport that speaks HTTP/2, including h2c
// prior knowledge. A *http.Transport is cloned so the custom proxy, dialer,
// and TLS settings survive; any other RoundTripper is reused unchanged and
// must support h2c itself.
//
// Parameters:
//   - base (http.RoundTripper): the primary transport, or nil.
//   - config (*tls.Config): the TLS configuration for a newly built transport.
//
// Returns:
//   - transport (http.RoundTripper): the fallback transport.
func newFallbackTransport(base http.RoundTripper, config *tls.Config) (transport http.RoundTripper) {
	if base == nil {
		transport = defaultFallbackTransport(config)

		return transport
	}

	if httpTransport, ok := base.(*http.Transport); ok {
		clone := httpTransport.Clone()

		clone.Protocols = newProtocols(false, true, true)

		transport = clone

		return transport
	}

	transport = base

	return transport
}

// defaultFallbackTransport builds the fallback transport used when the
// primary client has no transport to clone.
//
// Parameters:
//   - config (*tls.Config): the TLS configuration; nil uses system defaults.
//
// Returns:
//   - transport (*http.Transport): an h2c-capable transport with keep-alives
//     disabled.
func defaultFallbackTransport(config *tls.Config) (transport *http.Transport) {
	transport = DefaultHTTPTransport()

	transport.Protocols = newProtocols(false, true, true)
	transport.TLSClientConfig = cloneTLSConfig(config)

	return transport
}

// DefaultHTTPTransport returns a new *http.Transport derived from
// [DefaultHTTPPooledTransport] but with keep-alives and idle connections
// disabled, so no connection is ever reused.
//
// It suits transient clients and host-spraying scenarios, where connection
// reuse across changing hosts is unwanted.
//
// Returns:
//   - transport (*http.Transport): a newly configured non-pooled transport.
func DefaultHTTPTransport() (transport *http.Transport) {
	transport = DefaultHTTPPooledTransport()

	transport.DisableKeepAlives = true
	transport.MaxIdleConnsPerHost = -1

	return transport
}

// DefaultHTTPPooledTransport returns a new *http.Transport configured for
// connection pooling, with HTTP/2 support negotiated automatically.
//
// It is optimized for clients making repeated requests to the same hosts,
// allowing efficient reuse of TCP connections.
//
// Warning: the transport is intended for long-lived clients. For transient
// clients, consider [DefaultHTTPTransport] to avoid leaking file descriptors.
//
// Returns:
//   - transport (*http.Transport): a newly configured pooled transport.
func DefaultHTTPPooledTransport() (transport *http.Transport) {
	transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   runtime.GOMAXPROCS(0) + 1,
	}

	return transport
}

// newProtocols builds an [http.Protocols] set.
//
// Parameters:
//   - http1 (bool): whether HTTP/1.x is enabled.
//   - http2 (bool): whether HTTP/2 over TLS is enabled.
//   - unencryptedHTTP2 (bool): whether h2c is enabled.
//
// Returns:
//   - protocols (*http.Protocols): the configured set.
func newProtocols(http1, http2, unencryptedHTTP2 bool) (protocols *http.Protocols) {
	protocols = new(http.Protocols)

	protocols.SetHTTP1(http1)
	protocols.SetHTTP2(http2)
	protocols.SetUnencryptedHTTP2(unencryptedHTTP2)

	return protocols
}

// cloneTLSConfig clones a TLS configuration so later caller mutations do not
// affect a built client.
//
// Parameters:
//   - config (*tls.Config): the configuration to clone; nil stays nil.
//
// Returns:
//   - clone (*tls.Config): the cloned configuration, or nil.
func cloneTLSConfig(config *tls.Config) (clone *tls.Config) {
	if config == nil {
		return nil
	}

	clone = config.Clone()

	return clone
}

// Get performs an HTTP GET request through the shared default client.
//
// Parameters:
//   - ctx (context.Context): the context governing the whole execution.
//   - rawURL (string): the target URL.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func Get(ctx context.Context, rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	client := defaultClient()

	res, err = client.Request().Context(ctx).Method(hqgohttpmethod.GET).URL(rawURL).WithOptions(ofs...).Do() //nolint:contextcheck // ctx is applied through RequestBuilder.Context.

	return res, err
}

// Head performs an HTTP HEAD request through the shared default client.
//
// Parameters:
//   - ctx (context.Context): the context governing the whole execution.
//   - rawURL (string): the target URL.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func Head(ctx context.Context, rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	client := defaultClient()

	res, err = client.Request().Context(ctx).Method(hqgohttpmethod.HEAD).URL(rawURL).WithOptions(ofs...).Do() //nolint:contextcheck // ctx is applied through RequestBuilder.Context.

	return res, err
}

// Post performs an HTTP POST request with a body through the shared default
// client. See [Client.Post] for the accepted body types.
//
// Parameters:
//   - ctx (context.Context): the context governing the whole execution.
//   - rawURL (string): the target URL.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func Post(ctx context.Context, rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	client := defaultClient()

	res, err = client.Request().Context(ctx).Method(hqgohttpmethod.POST).URL(rawURL).Body(body).WithOptions(ofs...).Do() //nolint:contextcheck // ctx is applied through RequestBuilder.Context.

	return res, err
}

// Query performs an HTTP QUERY request with a body through the shared default
// client. See [Client.Query] for the accepted body types and retry behavior.
//
// Parameters:
//   - ctx (context.Context): the context governing the whole execution.
//   - rawURL (string): the target URL.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func Query(ctx context.Context, rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	client := defaultClient()

	res, err = client.Request().Context(ctx).Method(hqgohttpmethod.QUERY).URL(rawURL).Body(body).WithOptions(ofs...).Do() //nolint:contextcheck // ctx is applied through RequestBuilder.Context.

	return res, err
}

// Put performs an HTTP PUT request with a body through the shared default
// client. See [Client.Post] for the accepted body types.
//
// Parameters:
//   - ctx (context.Context): the context governing the whole execution.
//   - rawURL (string): the target URL.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func Put(ctx context.Context, rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	client := defaultClient()

	res, err = client.Request().Context(ctx).Method(hqgohttpmethod.PUT).URL(rawURL).Body(body).WithOptions(ofs...).Do() //nolint:contextcheck // ctx is applied through RequestBuilder.Context.

	return res, err
}

// Patch performs an HTTP PATCH request with a body through the shared default
// client. See [Client.Post] for the accepted body types.
//
// Parameters:
//   - ctx (context.Context): the context governing the whole execution.
//   - rawURL (string): the target URL.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func Patch(ctx context.Context, rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	client := defaultClient()

	res, err = client.Request().Context(ctx).Method(hqgohttpmethod.PATCH).URL(rawURL).Body(body).WithOptions(ofs...).Do() //nolint:contextcheck // ctx is applied through RequestBuilder.Context.

	return res, err
}

// Delete performs an HTTP DELETE request through the shared default client.
//
// Parameters:
//   - ctx (context.Context): the context governing the whole execution.
//   - rawURL (string): the target URL.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func Delete(ctx context.Context, rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	client := defaultClient()

	res, err = client.Request().Context(ctx).Method(hqgohttpmethod.DELETE).URL(rawURL).WithOptions(ofs...).Do() //nolint:contextcheck // ctx is applied through RequestBuilder.Context.

	return res, err
}

// Options performs an HTTP OPTIONS request through the shared default client.
//
// Parameters:
//   - ctx (context.Context): the context governing the whole execution.
//   - rawURL (string): the target URL.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func Options(ctx context.Context, rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	client := defaultClient()

	res, err = client.Request().Context(ctx).Method(hqgohttpmethod.OPTIONS).URL(rawURL).WithOptions(ofs...).Do() //nolint:contextcheck // ctx is applied through RequestBuilder.Context.

	return res, err
}
