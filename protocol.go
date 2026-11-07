package http

import "time"

const (
	// DefaultTimeout bounds an execution through response headers.
	DefaultTimeout = 30 * time.Second
	// DefaultResponseBodyTimeout disables the separate body-read bound.
	DefaultResponseBodyTimeout = 0 * time.Second
	// DefaultResponseReadLimit bounds bytes drained from a retried response.
	DefaultResponseReadLimit = 4096
	// DefaultUserAgent is sent when the application does not replace it.
	DefaultUserAgent = "hq-lib-http-go (https://github.com/hueristiq/hq-lib-http-go)"
	// DefaultHTTP3FallbackTTL controls the HTTP/3 failure cache.
	DefaultHTTP3FallbackTTL = time.Minute
)

// Protocol selects the HTTP version used by a [Client].
type Protocol int

const (
	// ProtocolAuto negotiates HTTP/2 over TLS and supports h2c fallback.
	ProtocolAuto Protocol = iota
	// ProtocolAutoHTTP3 tries HTTP/3 before HTTP/2 or HTTP/1.1.
	ProtocolAutoHTTP3
	// ProtocolHTTP1 forces HTTP/1.x.
	ProtocolHTTP1
	// ProtocolHTTP2 forces HTTP/2, including h2c prior knowledge.
	ProtocolHTTP2
	// ProtocolHTTP3 forces HTTP/3 through the injected HTTP/3 client.
	ProtocolHTTP3
)

// String returns the protocol name.
func (p Protocol) String() (protocol string) {
	switch p {
	case ProtocolAuto:
		return "auto"
	case ProtocolAutoHTTP3:
		return "auto-http3"
	case ProtocolHTTP1:
		return "http/1.1"
	case ProtocolHTTP2:
		return "http/2"
	case ProtocolHTTP3:
		return "http/3"
	default:
		return "unknown"
	}
}
