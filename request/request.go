package request

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Request wraps an HTTP request so it integrates directly with the standard
// library and any HTTP tooling.
//
// [New] and [NewLimited] convert the body to a [*ReusableReadCloser] buffered in
// memory. The request's GetBody function returns a fresh reader over the
// payload, so retries, redirects, and explicit replays resend the full body.
// The HTTP client also resets the body before each retry attempt. [NewStream]
// instead uses a caller-owned streaming body with no replay function.
//
// Construct through [New], [NewLimited], or [NewStream]; the zero value is not
// usable.
//
// Fields:
//   - Request (*http.Request): the underlying HTTP request.
type Request struct {
	*http.Request
}

// Std returns the underlying standard-library request.
//
// Returns:
//   - request (*http.Request): the embedded request, or nil for a nil wrapper.
func (r *Request) Std() (request *http.Request) {
	if r == nil {
		return nil
	}

	return r.Request
}

// New creates a [Request] for the given method, URL, and body, bound to ctx.
//
// The URL is parsed with [net/url.Parse] by default, which is strict: it
// applies no default scheme and repairs no invalid percent escapes. Use
// [WithURLParser] to supply a parser that adds either behavior, or any other
// custom parsing. The body accepts a string, a []byte, an io.Reader, an
// io.ReadSeeker, a *bytes.Buffer, a func() (io.Reader, error), or a
// *ReusableReadCloser; it is buffered in memory and its length becomes the
// request's ContentLength. Byte slices, buffers, and reusable bodies are
// copied or cloned, so later caller mutations and cursor changes do not change
// the payload. A reader that implements io.Closer is closed after it has been
// consumed, including when reading fails. An empty buffered body is stored as
// http.NoBody, so the request is sent with Content-Length: 0 rather than
// chunked encoding. A nil body means no body.
//
// A nil ctx leaves the request without an explicit context, so a client-level
// default applies. An explicit [context.Background] is preserved and overrides
// that default.
//
// Request.Host is cleared: an empty Host means "use the URL host", which lets
// a client default or the caller set an explicit Host header without it being
// indistinguishable from the implicit value.
//
// Parameters:
//   - ctx (context.Context): the context associated with the request. Nil
//     means [context.Background].
//   - method (string): the HTTP method (e.g., "GET", "POST"); empty means
//     GET.
//   - rawURL (string): the target URL for the request.
//   - body (any): an optional request body.
//   - ofs (...OptionFunc): optional URL parsing options.
//
// Returns:
//   - req (*Request): the newly created request wrapper.
//   - err (error): non-nil when the URL does not parse or the body type is
//     unsupported.
func New(ctx context.Context, method, rawURL string, body any, ofs ...OptionFunc) (req *Request, err error) {
	req, err = NewLimited(ctx, method, rawURL, body, 0, ofs...)

	return req, err
}

// NewLimited creates a [Request] like [New] but refuses to buffer more than
// limit bytes of body. A non-positive limit means unlimited. The limit is
// enforced while the body is read, so an oversized reader is not fully
// retained. Byte slices, strings, and buffers are checked before copying. An
// empty buffered body is stored as http.NoBody, so it is sent with
// Content-Length: 0.
//
// Parameters:
//   - ctx (context.Context): the context associated with the request. Nil
//     means [context.Background].
//   - method (string): the HTTP method (e.g., "POST"); empty means GET.
//   - rawURL (string): the target URL for the request.
//   - body (any): an optional request body, in any form [New] accepts.
//   - limit (int64): the maximum buffered body size in bytes; non-positive
//     means unlimited.
//   - ofs (...OptionFunc): optional URL parsing options.
//
// Returns:
//   - req (*Request): the newly created request wrapper.
//   - err (error): non-nil when the URL does not parse, the body type is
//     unsupported, the body exceeds the limit ([ErrBodyTooLarge]), or reading
//     the body fails.
func NewLimited(ctx context.Context, method, rawURL string, body any, limit int64, ofs ...OptionFunc) (req *Request, err error) {
	internalHTTPRequest, err := newInternalRequest(ctx, method, rawURL, resolveParser(ofs))
	if err != nil {
		return nil, err
	}

	reusableBodyReadCloser, err := getReusableBodyReadCloser(body, limit)
	if err != nil {
		return nil, err
	}

	if reusableBodyReadCloser != nil {
		reusableBodyReadCloser.Reset()

		if len(reusableBodyReadCloser.data) == 0 {
			// A non-nil zero-length body makes net/http send
			// Transfer-Encoding: chunked; http.NoBody keeps Content-Length: 0.
			internalHTTPRequest.Body = http.NoBody
			internalHTTPRequest.GetBody = func() (body io.ReadCloser, err error) {
				body = http.NoBody

				return body, nil
			}
		} else {
			internalHTTPRequest.Body = reusableBodyReadCloser
			internalHTTPRequest.ContentLength = int64(len(reusableBodyReadCloser.data))
			internalHTTPRequest.GetBody = func() (body io.ReadCloser, err error) {
				body = reusableBodyReadCloser.clone()

				return body, nil
			}
		}
	}

	req = &Request{Request: internalHTTPRequest}

	return req, nil
}

// NewStream creates a [Request] whose body is read directly from body instead
// of being buffered. It is the escape hatch for uploads too large to hold in
// memory.
//
// The request has no GetBody function, so a body cannot be replayed: an
// approved retry stops with [ErrBodyNotReplayable] rather than sending the
// already-consumed stream again. contentLength must be the exact body length,
// or -1 when it is unknown (the request is then sent with chunked transfer
// encoding).
//
// The caller owns body and must close it if it is never sent; the transport
// closes it after a send.
//
// Parameters:
//   - ctx (context.Context): the context associated with the request. Nil
//     means [context.Background].
//   - method (string): the HTTP method; empty means GET.
//   - rawURL (string): the target URL.
//   - body (io.Reader): the request body, read as the request is sent. Nil
//     means no body.
//   - contentLength (int64): the exact body length, or -1 when unknown.
//   - ofs (...OptionFunc): optional URL parsing options.
//
// Returns:
//   - req (*Request): the newly created request wrapper.
//   - err (error): non-nil when the URL cannot be parsed.
func NewStream(ctx context.Context, method, rawURL string, body io.Reader, contentLength int64, ofs ...OptionFunc) (req *Request, err error) {
	internalHTTPRequest, err := newInternalRequest(ctx, method, rawURL, resolveParser(ofs))
	if err != nil {
		return nil, err
	}

	if body != nil {
		if readCloser, ok := body.(io.ReadCloser); ok {
			internalHTTPRequest.Body = readCloser
		} else {
			internalHTTPRequest.Body = io.NopCloser(body)
		}

		if contentLength < 0 {
			contentLength = -1
		}

		internalHTTPRequest.ContentLength = contentLength
	}

	req = &Request{Request: internalHTTPRequest}

	return req, nil
}

// newInternalRequest parses rawURL and builds the standard-library request
// shared by [New] and [NewStream]. The URL is assigned directly because
// NewRequestWithContext parses its argument with url.Parse, which would reject
// or re-encode a path produced by a custom parser.
//
// Parameters:
//   - ctx (context.Context): the request context; nil means Background.
//   - method (string): the HTTP method; empty means GET.
//   - rawURL (string): the target URL.
//   - parser (URLParser): the parser converting rawURL into an absolute URL.
//
// Returns:
//   - req (*http.Request): the standard-library request with an empty Host.
//   - err (error): non-nil when the URL or request cannot be parsed.
func newInternalRequest(ctx context.Context, method, rawURL string, parser URLParser) (req *http.Request, err error) {
	ctx = nonNilContext(ctx)

	parsedURL, err := parser.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if parsedURL == nil {
		return nil, errors.New("request.newInternalRequest: parser returned a nil URL")
	}

	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("request.newInternalRequest: URL %q is not absolute (missing scheme or host)", rawURL)
	}

	// This has no impact on the wire: the transport reads only the host from
	// the request — everything else travels in the assigned url.URL instance.
	req, err = http.NewRequestWithContext(ctx, method, parsedURL.Scheme+"://"+parsedURL.Host, nil) //nolint:gocritic // nil keeps Request.Body nil, matching net/http's no-body convention; http.NoBody would leak a non-nil body.
	if err != nil {
		return nil, err
	}

	req.URL = parsedURL

	// NewRequestWithContext copies the URL host into Request.Host. Clear it so
	// an empty Host means "use the URL host" and a client default or the
	// caller can set an explicit Host header without being indistinguishable
	// from the implicit value.
	req.Host = ""

	return req, nil
}

// explicitContext marks a caller-supplied context that is
// [context.Background], so the client does not replace it with its configured
// default.
type explicitContext struct{ context.Context } //nolint:containedctx // The wrapper forwards a caller-supplied context.

// nonNilContext normalizes ctx for http.NewRequestWithContext. A nil context
// becomes [context.Background] without marking it explicit, so the client
// default applies. An explicit [context.Background] is wrapped, so it overrides
// the client default.
//
// Parameters:
//   - ctx (context.Context): the context to normalize.
//
// Returns:
//   - normalized (context.Context): ctx, [context.Background] when ctx is nil,
//     or an explicit wrapper when ctx is [context.Background].
func nonNilContext(ctx context.Context) (normalized context.Context) {
	if ctx == nil {
		return context.Background()
	}

	if ctx == context.Background() {
		return explicitContext{Context: ctx}
	}

	return ctx
}

// getReusableBodyReadCloser converts the provided raw input into a reusable
// ReadCloser, enforcing limit bytes when limit is positive.
//
// Supported types include:
//   - *ReusableReadCloser: cloned, so the caller's cursor is left untouched.
//   - func() (io.Reader, error): the function is invoked to obtain an
//     io.Reader, which is then converted via NewReusableReadCloserLimited.
//   - Other types: raw is passed to NewReusableReadCloserLimited, which
//     supports a variety of types.
//
// Parameters:
//   - raw (any): the raw input representing the request body; nil yields no
//     body.
//   - limit (int64): the maximum buffered body size in bytes; non-positive
//     means unlimited.
//
// Returns:
//   - reader (*ReusableReadCloser): the reusable read-closer, or nil when raw
//     is nil.
//   - err (error): non-nil when the conversion fails or the body exceeds the
//     limit.
func getReusableBodyReadCloser(raw any, limit int64) (reader *ReusableReadCloser, err error) {
	if raw == nil {
		return nil, nil //nolint:nilnil // A nil body is valid and carries no error.
	}

	switch body := raw.(type) {
	case *ReusableReadCloser:
		if body == nil {
			return nil, nil //nolint:nilnil // A nil body is valid and carries no error.
		}

		if exceedsLimit(int64(len(body.data)), limit) {
			return nil, ErrBodyTooLarge
		}

		reader = body.clone()
	case func() (io.Reader, error):
		if body == nil {
			return nil, nil //nolint:nilnil // A nil body function yields no body.
		}

		var tmp io.Reader

		tmp, err = body()
		if err != nil {
			return nil, err
		}

		reader, err = NewReusableReadCloserLimited(tmp, limit)
		if err != nil {
			return nil, err
		}
	default:
		reader, err = NewReusableReadCloserLimited(body, limit)
		if err != nil {
			return nil, err
		}
	}

	return reader, nil
}
