package httpclient

import (
	"crypto/tls"
	"net"
	"net/http"
	"runtime"
	"time"
)

// DefaultHTTPClient returns a new non-pooled client.
func DefaultHTTPClient() (client *http.Client) {
	return &http.Client{Transport: DefaultHTTPTransport()}
}

// DefaultHTTPPooledClient returns a new client with the package's pooled
// transport defaults.
func DefaultHTTPPooledClient() (client *http.Client) {
	return &http.Client{Transport: DefaultHTTPPooledTransport()}
}

// newAutoHTTPClient builds the pooled client used by [ProtocolAuto]: HTTP/2
// negotiated over TLS with h2c fallback, on top of the pooled transport
// defaults.
func newAutoHTTPClient(config *tls.Config) (client *http.Client) {
	transport := DefaultHTTPPooledTransport()
	transport.TLSClientConfig = cloneTLSConfig(config)

	return &http.Client{Transport: transport}
}

// newFallbackClient clones base and swaps in the fallback transport, so the
// fallback path keeps the base client's timeouts, jar, and redirect policy.
// A nil base starts from a zero http.Client.
func newFallbackClient(base *http.Client, config *tls.Config) (client *http.Client) {
	if base == nil {
		base = &http.Client{}
	}

	clone := *base
	clone.Transport = newFallbackTransport(base.Transport, config)

	return &clone
}

// newHTTP1Client builds the pooled client used by [ProtocolHTTP1]: HTTP/1.x
// only, with HTTP/2 negotiation disabled.
func newHTTP1Client(config *tls.Config) (client *http.Client) {
	transport := DefaultHTTPPooledTransport()
	transport.ForceAttemptHTTP2 = false
	transport.Protocols = newProtocols(true, false, false)
	transport.TLSClientConfig = cloneTLSConfig(config)

	return &http.Client{Transport: transport}
}

// newHTTP2Client builds the pooled client used by [ProtocolHTTP2]: HTTP/2
// forced, including h2c prior knowledge.
func newHTTP2Client(config *tls.Config) (client *http.Client) {
	transport := DefaultHTTPPooledTransport()
	transport.ForceAttemptHTTP2 = true
	transport.Protocols = newProtocols(false, true, true)
	transport.TLSClientConfig = cloneTLSConfig(config)

	return &http.Client{Transport: transport}
}

// newFallbackTransport derives the fallback transport from base: a nil base
// yields a fresh non-pooled HTTP/2-capable transport, an *http.Transport is
// cloned with HTTP/2 and h2c enabled, and anything else is used as is.
func newFallbackTransport(base http.RoundTripper, config *tls.Config) (transport http.RoundTripper) {
	if base == nil {
		fallback := DefaultHTTPTransport()
		fallback.Protocols = newProtocols(false, true, true)
		fallback.TLSClientConfig = cloneTLSConfig(config)

		return fallback
	}

	if httpTransport, ok := base.(*http.Transport); ok {
		clone := httpTransport.Clone()
		clone.Protocols = newProtocols(false, true, true)

		return clone
	}

	return base
}

// DefaultHTTPTransport returns a new transport with keep-alives disabled.
func DefaultHTTPTransport() (transport *http.Transport) {
	transport = DefaultHTTPPooledTransport()
	transport.DisableKeepAlives = true
	transport.MaxIdleConnsPerHost = -1

	return transport
}

// DefaultHTTPPooledTransport returns a new transport with bounded pooling,
// HTTP/2 negotiation, environment proxying, and production timeouts.
func DefaultHTTPPooledTransport() (transport *http.Transport) {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   runtime.GOMAXPROCS(0) + 1,
	}
}

// newProtocols assembles an [http.Protocols] value from the three toggles.
func newProtocols(http1, http2, unencryptedHTTP2 bool) (protocols *http.Protocols) {
	protocols = new(http.Protocols)
	protocols.SetHTTP1(http1)
	protocols.SetHTTP2(http2)
	protocols.SetUnencryptedHTTP2(unencryptedHTTP2)

	return protocols
}

// cloneTLSConfig returns a deep copy of config so the library never mutates
// a caller-supplied TLS configuration. Nil stays nil.
func cloneTLSConfig(config *tls.Config) (clone *tls.Config) {
	if config == nil {
		return nil
	}

	return config.Clone()
}
