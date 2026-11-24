package raw

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"time"

	hqgoerrors "github.com/hueristiq/hq-lib-errors-go"
)

// DefaultTimeout is the default bound on a whole exchange — dial, write, and
// response read — applied when the context carries no earlier deadline and
// [WithTimeout] is not supplied.
const DefaultTimeout = 30 * time.Second

// Options holds the configurable knobs of a [Do] exchange, populated through
// [OptionFunc] values.
type Options struct {
	UseTLS    bool
	TLSConfig *tls.Config
	Timeout   time.Duration
}

// OptionFunc configures a [Do] exchange through the functional-options idiom.
//
// When the same option is supplied more than once, the last one wins.
type OptionFunc func(opts *Options)

// WithTLS returns an [OptionFunc] that selects whether the connection is
// wrapped in TLS. When a [WithTLSConfig] option is supplied, TLS is implied.
func WithTLS(enabled bool) (f OptionFunc) {
	return func(opts *Options) {
		opts.UseTLS = enabled
	}
}

// WithTLSConfig returns an [OptionFunc] that sets the TLS configuration and
// implies TLS. The configuration is cloned before use, so later mutations of
// the caller's value never affect the exchange; an empty ServerName is filled
// with the host portion of the address.
func WithTLSConfig(config *tls.Config) (f OptionFunc) {
	return func(opts *Options) {
		opts.TLSConfig = config
		opts.UseTLS = true
	}
}

// WithTimeout returns an [OptionFunc] that bounds the whole exchange — dial,
// TLS handshake, write, and response read — when the context carries no
// earlier deadline. Without any deadline the exchange can block forever.
func WithTimeout(timeout time.Duration) (f OptionFunc) {
	return func(opts *Options) {
		opts.Timeout = timeout
	}
}

// responseBody binds a response body to its connection: closing the body
// closes the connection. A raw exchange serves exactly one request on its
// connection, so the connection has no use after the body.
type responseBody struct {
	body io.ReadCloser
	conn net.Conn
}

// Compile-time guard ensuring [responseBody] satisfies [io.ReadCloser].
var _ io.ReadCloser = (*responseBody)(nil)

// Read reads from the response body.
func (b *responseBody) Read(p []byte) (n int, err error) {
	n, err = b.body.Read(p)

	return
}

// Close closes the response body and the connection it is read from.
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
// configured Timeout; the bound covers the dial, the TLS handshake, writing,
// and reading the response body, so consume it before the deadline passes.
// Closing the response body closes the connection.
func Do(ctx context.Context, address string, request []byte, ofs ...OptionFunc) (res *http.Response, err error) {
	if address == "" {
		err = hqgoerrors.New("address is empty")

		return
	}

	if len(request) == 0 {
		err = hqgoerrors.New("request is empty")

		return
	}

	opts := &Options{
		Timeout: DefaultTimeout,
	}

	for _, of := range ofs {
		of(opts)
	}

	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}

	deadline := time.Now().Add(opts.Timeout)

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
		err = hqgoerrors.Wrapf(err, "dialing %s", address)

		return
	}

	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	_ = conn.SetDeadline(deadline)

	if err = writeRequest(conn, request); err != nil {
		err = hqgoerrors.Wrap(err, "writing request")

		return
	}

	res, err = http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: requestMethod(request)})
	if err != nil {
		err = hqgoerrors.Wrap(err, "reading response")

		return
	}

	res.Body = &responseBody{body: res.Body, conn: conn}

	return
}

// dial establishes the connection for an exchange: TCP with a context-aware
// dial, then a TLS handshake when configured.
func dial(ctx context.Context, address string, opts *Options) (conn net.Conn, err error) {
	dialer := &net.Dialer{}

	conn, err = dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return
	}

	if !opts.UseTLS {
		return
	}

	config := opts.TLSConfig

	if config == nil {
		config = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	} else {
		config = config.Clone()
	}

	if config.ServerName == "" {
		if host, _, serr := net.SplitHostPort(address); serr == nil {
			config.ServerName = host
		}
	}

	tlsConn := tls.Client(conn, config)

	if err = tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()

		return
	}

	conn = tlsConn

	return
}

// writeRequest writes the request bytes in full.
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
func requestMethod(request []byte) (method string) {
	line, _, _ := bytes.Cut(request, []byte("\r\n"))

	token, _, _ := bytes.Cut(line, []byte(" "))

	method = string(token)

	return
}
