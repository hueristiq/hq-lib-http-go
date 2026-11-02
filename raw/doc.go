// Package raw sends raw HTTP/1.x request bytes over freshly dialed
// connections.
//
// It suits work where the exact bytes on the wire matter — security tooling,
// protocol probing, reproducing a captured request byte for byte — and where
// the standard client's normalization (header canonicalization, connection
// reuse, redirects) would get in the way.
//
// # Entry points
//
// [Do] dials a host:port address, writes the request bytes exactly as given,
// and parses the response. One exchange gets one connection: no retry,
// redirect, or pooling machinery applies, and closing the response body
// closes the connection.
//
// # Configuration
//
// Behavior is set through functional options:
//
//   - [WithTLS] and [WithTLSConfig] wrap the connection in TLS.
//   - [WithTimeout] bounds the exchange — dial, TLS handshake, write, and
//     response read — when the context carries no earlier deadline;
//     [DefaultTimeout] applies otherwise.
//
// The implementation relies on the Go standard library only.
package raw
