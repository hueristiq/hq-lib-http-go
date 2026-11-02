package raw

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// DefaultTimeout is the default bound on a whole exchange — dial, write, and
// response read — applied when the context carries no earlier deadline and
// [WithTimeout] is not supplied.
const DefaultTimeout = 30 * time.Second

// options holds the configurable knobs of a [Do] exchange, populated through
// [OptionFunc] values.
//
// Fields:
//   - useTLS (bool): whether the connection is wrapped in TLS.
//   - tlsConfig (*tls.Config): the TLS configuration; nil means a secure
//     default derived from the address.
//   - timeout (time.Duration): the exchange bound when the context carries
//     no earlier deadline. Non-positive means [DefaultTimeout].
type options struct {
	useTLS    bool
	tlsConfig *tls.Config
	timeout   time.Duration
}

// OptionFunc configures a [Do] exchange through the functional-options idiom.
//
// When the same option is supplied more than once, the last one wins.
//
// Parameters:
//   - opts (*options): the options bag to mutate.
type OptionFunc func(opts *options)

// WithTLS returns an [OptionFunc] that selects whether the connection is
// wrapped in TLS. When a [WithTLSConfig] option is supplied, TLS is implied.
//
// Parameters:
//   - enabled (bool): true to dial TLS, false for cleartext TCP.
//
// Returns:
//   - f (OptionFunc): a functional option that sets the useTLS field.
func WithTLS(enabled bool) (f OptionFunc) {
	return func(opts *options) {
		opts.useTLS = enabled
	}
}

// WithTLSConfig returns an [OptionFunc] that sets the TLS configuration and
// implies TLS. The configuration is cloned before use, so later mutations of
// the caller's value never affect the exchange; an empty ServerName is filled
// with the host portion of the address.
//
// Parameters:
//   - config (*tls.Config): the TLS configuration. Nil selects a secure
//     default with TLS 1.2 as the minimum version.
//
// Returns:
//   - f (OptionFunc): a functional option that sets the tlsConfig field.
func WithTLSConfig(config *tls.Config) (f OptionFunc) {
	return func(opts *options) {
		opts.tlsConfig = config
		opts.useTLS = true
	}
}

// WithTimeout returns an [OptionFunc] that bounds the whole exchange — dial,
// TLS handshake, write, and response read — when the context carries no
// earlier deadline. Without any deadline the exchange can block forever.
//
// Parameters:
//   - timeout (time.Duration): the exchange bound. Values less than or equal
//     to 0 fall back to [DefaultTimeout].
//
// Returns:
//   - f (OptionFunc): a functional option that sets the timeout field.
func WithTimeout(timeout time.Duration) (f OptionFunc) {
	return func(opts *options) {
		opts.timeout = timeout
	}
}

// responseBody binds a response body to its connection: closing the body
// closes the connection. A raw exchange serves exactly one request on its
// connection, so the connection has no use after the body.
//
// Fields:
//   - body (io.ReadCloser): the parsed response body.
//   - conn (net.Conn): the connection the body is read from.
type responseBody struct {
	body io.ReadCloser
	conn net.Conn
}

// Compile-time guard ensuring [responseBody] satisfies [io.ReadCloser].
var _ io.ReadCloser = (*responseBody)(nil)

// Read reads from the response body.
//
// Parameters:
//   - p ([]byte): the buffer to read into.
//
// Returns:
//   - n (int): the number of bytes read.
//   - err (error): non-nil when the read fails.
func (b *responseBody) Read(p []byte) (n int, err error) {
	n, err = b.body.Read(p)

	return
}

// Close closes the response body and the connection it is read from.
//
// Returns:
//   - err (error): the first close failure, if any.
func (b *responseBody) Close() (err error) {
	if err = b.body.Close(); err != nil {
		_ = b.conn.Close()

		return
	}

	err = b.conn.Close()

	return
}

// Do sends raw HTTP/1.x request bytes to address over a freshly dialed
// connection — cleartext TCP by default, TLS with [WithTLS] or
// [WithTLSConfig] — and parses the response.
//
// The request bytes are sent exactly as given: the caller owns the request
// line, every header (including Host and, for a body, Content-Length), and
// the body. No retry, redirect, or connection-pooling machinery applies; one
// exchange gets one connection.
//
// The exchange is bounded by the earlier of the context's deadline and the
// configured timeout; the bound covers the dial, the TLS handshake, writing,
// and reading the response body, so consume it before the deadline passes.
// Closing the response body closes the connection.
//
// Parameters:
//   - ctx (context.Context): the context governing the exchange.
//   - address (string): the host:port pair to connect to.
//   - request ([]byte): the raw HTTP/1.x request bytes.
//   - ofs (...OptionFunc): functional options configuring the exchange.
//
// Returns:
//   - res (*http.Response): the parsed response; the caller owns and must
//     close its body, which closes the connection.
//   - err (error): non-nil when the exchange fails.
func Do(ctx context.Context, address string, request []byte, ofs ...OptionFunc) (res *http.Response, err error) {
	if address == "" {
		err = errors.New("raw.Do: address is empty")

		return
	}

	if len(request) == 0 {
		err = errors.New("raw.Do: request is empty")

		return
	}

	opts := &options{
		timeout: DefaultTimeout,
	}

	for _, of := range ofs {
		of(opts)
	}

	if opts.timeout <= 0 {
		opts.timeout = DefaultTimeout
	}

	deadline := time.Now().Add(opts.timeout)

	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	// The dial and the TLS handshake run under a context carrying the exchange
	// deadline, so neither can outlive the bound the rest of the exchange
	// observes.
	dialCtx, cancel := context.WithDeadline(ctx, deadline)

	defer cancel()

	conn, err := dial(dialCtx, address, opts)
	if err != nil {
		err = fmt.Errorf("raw.Do: dialing %s: %w", address, err)

		return
	}

	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	_ = conn.SetDeadline(deadline)

	if err = writeRequest(conn, request); err != nil {
		err = fmt.Errorf("raw.Do: writing request: %w", err)

		return
	}

	res, err = http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: requestMethod(request)})
	if err != nil {
		err = fmt.Errorf("raw.Do: reading response: %w", err)

		return
	}

	res.Body = &responseBody{body: res.Body, conn: conn}

	return
}

// dial establishes the connection for an exchange: TCP with a context-aware
// dial, then a TLS handshake when configured.
//
// Parameters:
//   - ctx (context.Context): bounds the dial and the handshake.
//   - address (string): the host:port pair to connect to.
//   - opts (*options): the exchange configuration.
//
// Returns:
//   - conn (net.Conn): the connected, possibly TLS-wrapped connection.
//   - err (error): non-nil when dialing or the handshake fails.
func dial(ctx context.Context, address string, opts *options) (conn net.Conn, err error) {
	dialer := &net.Dialer{}

	conn, err = dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return
	}

	if !opts.useTLS {
		return
	}

	tlsConfig := opts.tlsConfig

	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	} else {
		tlsConfig = tlsConfig.Clone()
	}

	if tlsConfig.ServerName == "" {
		if host, _, serr := net.SplitHostPort(address); serr == nil {
			tlsConfig.ServerName = host
		}
	}

	tlsConn := tls.Client(conn, tlsConfig)

	if err = tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()

		return
	}

	conn = tlsConn

	return
}

// writeRequest writes the request bytes in full.
//
// Parameters:
//   - conn (net.Conn): the connection to write to.
//   - request ([]byte): the raw HTTP/1.x request bytes.
//
// Returns:
//   - err (error): non-nil when a write fails before all bytes are sent.
func writeRequest(conn net.Conn, request []byte) (err error) {
	pending := request

	for len(pending) > 0 {
		n, werr := conn.Write(pending)
		if werr != nil {
			err = werr

			return
		}

		pending = pending[n:]
	}

	return
}

// requestMethod extracts the method from the request line, so the response
// parser applies the right body semantics — a HEAD response carries no body.
//
// Parameters:
//   - request ([]byte): the raw HTTP/1.x request bytes.
//
// Returns:
//   - method (string): the request method, or "" when the request line does
//     not parse; the response parser treats that like GET.
func requestMethod(request []byte) (method string) {
	line, _, _ := bytes.Cut(request, []byte("\r\n"))

	token, _, _ := bytes.Cut(line, []byte(" "))

	method = string(token)

	return
}
