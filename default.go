package http

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

func newAutoHTTPClient(config *tls.Config) (client *http.Client) {
	transport := DefaultHTTPPooledTransport()
	transport.TLSClientConfig = cloneTLSConfig(config)

	return &http.Client{Transport: transport}
}

func newFallbackClient(base *http.Client, config *tls.Config) (client *http.Client) {
	if base == nil {
		base = &http.Client{}
	}

	clone := *base
	clone.Transport = newFallbackTransport(base.Transport, config)

	return &clone
}

func newHTTP1Client(config *tls.Config) (client *http.Client) {
	transport := DefaultHTTPPooledTransport()
	transport.ForceAttemptHTTP2 = false
	transport.Protocols = newProtocols(true, false, false)
	transport.TLSClientConfig = cloneTLSConfig(config)

	return &http.Client{Transport: transport}
}

func newHTTP2Client(config *tls.Config) (client *http.Client) {
	transport := DefaultHTTPPooledTransport()
	transport.ForceAttemptHTTP2 = true
	transport.Protocols = newProtocols(false, true, true)
	transport.TLSClientConfig = cloneTLSConfig(config)

	return &http.Client{Transport: transport}
}

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

func newProtocols(http1, http2, unencryptedHTTP2 bool) (protocols *http.Protocols) {
	protocols = new(http.Protocols)
	protocols.SetHTTP1(http1)
	protocols.SetHTTP2(http2)
	protocols.SetUnencryptedHTTP2(unencryptedHTTP2)

	return protocols
}

func cloneTLSConfig(config *tls.Config) (clone *tls.Config) {
	if config == nil {
		return nil
	}

	return config.Clone()
}
