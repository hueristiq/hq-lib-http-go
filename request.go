package http

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	hqgohttpheader "github.com/hueristiq/hq-lib-http-go/header"
	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
	hqgohttpmime "github.com/hueristiq/hq-lib-http-go/mime"
	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
	hqgoretrierbackoff "github.com/hueristiq/hq-lib-retrier-go/backoff"
)

// RequestOptions holds the per-request overrides of the execution knobs of a
// [Client], populated through [RequestOptionFunc] values passed to
// [Client.Do], the convenience methods, or [RequestBuilder.WithOptions].
//
// The values seed from the client configuration; every option replaces the
// seeded value it touches.
//
// Fields:
//   - AttemptTimeout (time.Duration): the bound on one attempt through receipt
//     of the response headers. Non-positive disables it.
//   - MaxResponseBodySize (int64): the cap on bytes buffered by the [Body]
//     helpers. Zero means unlimited.
//   - MaxRequestBodySize (int64): the cap on bytes buffered into a request body.
//     Zero means unlimited.
//   - MaxRetries (int): the number of retries after the initial attempt.
//   - RetryWaitMin (time.Duration): the minimum delay between attempts.
//   - RetryWaitMax (time.Duration): the maximum delay between attempts, also
//     capping an honored Retry-After wait.
//   - NewRetryBackoff (func(minDelay, maxDelay time.Duration) (backoff Backoff)):
//     the constructor for the strategy computing the delay per attempt.
//   - RetryPolicy (RetryPolicy): the predicate deciding, per attempt, whether to
//     retry.
//   - RateLimiter (RateLimiter): the limiter waited on before every attempt.
//   - OnRequest (OnRequestFunc): the callback invoked before each attempt.
//   - OnResponse (OnResponseFunc): the callback invoked after each attempt.
//   - OnRetry (OnRetryFunc): the callback invoked before each retry wait.
//   - PrepareRetry (PrepareRetryFunc): the hook invoked after the retry wait and
//     before the next attempt.
//   - RetryExhaustionHandler (RetryExhaustionHandler): the callback deciding what
//     to return when retries run out.
//   - IdempotentRetries (bool): whether only idempotent methods are retried.
//   - ResponseReadLimit (int64): the maximum number of bytes drained from a
//     retried response's body.
//   - RetryErrorBodyLimit (int64): the maximum number of bytes copied into a
//     [RetryError] from the final response body. Zero disables body capture.
type RequestOptions struct {
	AttemptTimeout         time.Duration
	MaxResponseBodySize    int64
	MaxRequestBodySize     int64
	MaxRetries             int
	RetryWaitMin           time.Duration
	RetryWaitMax           time.Duration
	NewRetryBackoff        func(minDelay, maxDelay time.Duration) (backoff Backoff)
	RetryPolicy            RetryPolicy
	RateLimiter            RateLimiter
	OnRequest              OnRequestFunc
	OnResponse             OnResponseFunc
	OnRetry                OnRetryFunc
	PrepareRetry           PrepareRetryFunc
	RetryExhaustionHandler RetryExhaustionHandler
	IdempotentRetries      bool
	ResponseReadLimit      int64
	RetryErrorBodyLimit    int64
}

// normalize fills invalid values in the request options with their defaults.
// The request options seed from an already normalized client configuration,
// so this only guards hand-built executions.
func (opts *RequestOptions) normalize() {
	if opts.AttemptTimeout < 0 {
		opts.AttemptTimeout = 0
	}

	if opts.MaxRetries < 0 {
		opts.MaxRetries = DefaultRetryMax
	}

	normalizeRetryBounds(&opts.RetryWaitMin, &opts.RetryWaitMax)

	if opts.NewRetryBackoff == nil {
		opts.NewRetryBackoff = hqgoretrierbackoff.ExponentialWithDecorrelatedJitter
	}

	if opts.RetryPolicy == nil {
		opts.RetryPolicy = DefaultRetryPolicy
	}

	if opts.ResponseReadLimit <= 0 {
		opts.ResponseReadLimit = DefaultResponseReadLimit
	}

	if opts.RetryErrorBodyLimit < 0 {
		opts.RetryErrorBodyLimit = 0
	}
}

// RequestOptionFunc configures a single request execution through the
// functional-options idiom, overriding the matching client configuration for
// that execution only.
//
// When the same option is supplied more than once, the last one wins.
//
// Parameters:
//   - opts (*RequestOptions): the options bag to mutate.
type RequestOptionFunc func(opts *RequestOptions)

// HeaderField is a single HTTP header field kept in insertion order.
//
// Fields:
//   - Key (string): the header field name.
//   - Value (string): the header field value.
type HeaderField struct {
	Key   string
	Value string
}

// headerOp is a header operation queued by [RequestBuilder.SetHeader] or
// [RequestBuilder.AddHeader], kept in call order.
//
// Fields:
//   - key (string): the header field name.
//   - value (string): the header field value.
//   - add (bool): true for AddHeader, false for SetHeader.
type headerOp struct {
	key   string
	value string
	add   bool
}

// requestOptions resolves the execution configuration for a single request:
// the client configuration overlaid with the given overrides.
//
// Parameters:
//   - ofs (...RequestOptionFunc): the per-request overrides.
//
// Returns:
//   - opts (*RequestOptions): the resolved, normalized configuration.
func (c *Client) requestOptions(ofs ...RequestOptionFunc) (opts *RequestOptions) {
	opts = &RequestOptions{
		AttemptTimeout:         c.AttemptTimeout,
		MaxResponseBodySize:    c.MaxResponseBodySize,
		MaxRequestBodySize:     c.MaxRequestBodySize,
		MaxRetries:             c.MaxRetries,
		RetryWaitMin:           c.RetryWaitMin,
		RetryWaitMax:           c.RetryWaitMax,
		NewRetryBackoff:        c.NewRetryBackoff,
		RetryPolicy:            c.RetryPolicy,
		RateLimiter:            c.RateLimiter,
		OnRequest:              c.OnRequest,
		OnResponse:             c.OnResponse,
		OnRetry:                c.OnRetry,
		PrepareRetry:           c.PrepareRetry,
		RetryExhaustionHandler: c.RetryExhaustionHandler,
		IdempotentRetries:      c.IdempotentRetries,
		ResponseReadLimit:      c.ResponseReadLimit,
		RetryErrorBodyLimit:    c.RetryErrorBodyLimit,
	}

	for _, of := range ofs {
		if of != nil {
			of(opts)
		}
	}

	opts.normalize()

	return opts
}

// RequestBuilder is an HTTP request under construction.
//
// The builder methods return the receiver for chaining — an intentional
// fluent-API deviation from the named-return convention; the deviation
// applies to the fluent setters only.
//
// A builder is bound to the [Client] that created it and executes through it:
// the client's retry engine, rate limiter, callbacks, and HTTP/2 fallback all
// apply. Construct through [Client.Request]; the zero value is not usable.
// A builder is not safe for concurrent use.
//
// Fields:
//   - client (*Client): the client executing the built request.
//   - method (hqgohttpmethod.Method): the HTTP method; empty means GET.
//   - rawURL (string): the request URL, used as given.
//   - params (url.Values): query parameters merged into the URL's query.
//   - headers ([]headerOp): header set and add operations in call order.
//   - body (any): the request payload, in any form the request package
//     accepts; nil means no body. It is cleared when an encoder is set.
//   - bodyErr (error): an error from buffering a reader-backed body when
//     [RequestBuilder.Body] was called; surfaced by Do.
//   - bodyEncoder (func(limit int64) ([]byte, string, error)): the JSON, XML,
//     form, or multipart encoder chosen by the latest body setter, or nil when
//     the raw body is used. A positive limit bounds the encoded body; the
//     multipart encoder stops writing once it is exceeded.
//   - bodyStream (io.Reader): the streaming body source set by
//     [RequestBuilder.BodyStream], or nil.
//   - bodyStreamSize (int64): the streaming body length, or -1 when unknown.
//   - ctx (context.Context): the default execution context, used when Do is
//     called with a nil context.
//   - requestOptions ([]RequestOptionFunc): per-request overrides of the
//     execution knobs.
type RequestBuilder struct {
	client         *Client
	method         hqgohttpmethod.Method
	rawURL         string
	params         url.Values
	headers        []headerOp
	body           any
	bodyErr        error
	bodyEncoder    func(limit int64) ([]byte, string, error)
	bodyStream     io.Reader
	bodyStreamSize int64
	ctx            context.Context //nolint:containedctx // Context stores a per-request context by design.
	requestOptions []RequestOptionFunc
}

// Method sets the HTTP method. Unset means GET.
//
// Parameters:
//   - method (hqgohttpmethod.Method): the HTTP method, e.g. method.POST.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) Method(method hqgohttpmethod.Method) (builder *RequestBuilder) {
	b.method = method

	return b
}

// URL sets the request URL. It is parsed and used as given: a URL carrying a
// URI scheme, a host:port authority, or a scheme-less host such as
// "example.com/path" (which defaults to http). A bare relative path has no
// host and is rejected when the request is built.
//
// Parameters:
//   - rawURL (string): the request URL.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) URL(rawURL string) (builder *RequestBuilder) {
	b.rawURL = rawURL

	return b
}

// Param appends a query parameter to the URL's query string. Repeated calls
// with the same key add multiple values. Existing raw-query text is preserved;
// the encoded parameter is appended rather than rewriting the whole query.
//
// Parameters:
//   - key (string): the parameter name.
//   - value (string): the parameter value.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) Param(key, value string) (builder *RequestBuilder) {
	b.params.Add(key, value)

	return b
}

// SetHeader sets a header, replacing a client default and all previously set
// values of the same key when the request is built. Host maps to Request.Host;
// Content-Length, Transfer-Encoding, and connection-specific headers are
// managed by net/http and cannot be overridden this way.
//
// Parameters:
//   - key (string): the header field name.
//   - value (string): the header field value.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) SetHeader(key, value string) (builder *RequestBuilder) {
	b.headers = append(b.headers, headerOp{key: key, value: value})

	return b
}

// AddHeader appends a header value to the client default and to any other
// values of the same key already present when the request is built. Host maps
// to Request.Host and is singular; the last Host value supplied wins.
//
// Parameters:
//   - key (string): the header field name.
//   - value (string): the header field value.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) AddHeader(key, value string) (builder *RequestBuilder) {
	b.headers = append(b.headers, headerOp{key: key, value: value, add: true})

	return b
}

// Body sets the request payload, in any form the request package accepts: a
// string, a []byte, an io.Reader, an io.ReadSeeker, a *bytes.Buffer, a
// func() (io.Reader, error), or a *ReusableReadCloser. A reader is buffered
// when Body is called, so every retry attempt and every later Do resends it in
// full. A read error from the reader is reported by Do.
//
// Parameters:
//   - body (any): the request payload; nil means no body.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) Body(body any) (builder *RequestBuilder) {
	b.body = body
	b.bodyErr = nil
	b.bodyEncoder = nil
	b.bodyStream = nil
	b.bodyStreamSize = 0

	switch body.(type) {
	case nil, string, []byte, *[]byte, *bytes.Buffer, func() (io.Reader, error), *hqgohttprequest.ReusableReadCloser:
		return b
	}

	if _, ok := body.(io.Reader); !ok {
		return b
	}

	reusable, err := hqgohttprequest.NewReusableReadCloser(body)
	if err != nil {
		b.body = nil
		b.bodyErr = err

		return b
	}

	b.body = reusable

	return b
}

// BodyStream sets a request body that is read directly from body as the
// request is sent, instead of being buffered in memory. It is the escape hatch
// for uploads too large to hold in memory.
//
// A streamed body has no replay function: an approved retry stops after the
// first attempt with a [*RetryError] matching [ErrBodyNotReplayable].
// contentLength must be the exact body length, or -1 when it is unknown (the
// request is then sent with chunked transfer encoding). The caller owns reader and must close it if the
// request is never sent. It replaces any body set by [RequestBuilder.Body],
// [JSON], [XML], [Form], or [Multipart].
//
// Parameters:
//   - reader (io.Reader): the body source; nil means no body.
//   - contentLength (int64): the exact body length, or -1 when unknown.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) BodyStream(reader io.Reader, contentLength int64) (builder *RequestBuilder) {
	b.body = nil
	b.bodyErr = nil
	b.bodyEncoder = nil
	b.bodyStream = reader
	b.bodyStreamSize = contentLength

	return b
}

// JSON encodes value as the JSON request body. Encoding happens when the
// request is built, so marshal errors surface from [RequestBuilder.Do]. The
// Content-Type is set to application/json unless the final request already
// carries one. It replaces any body set by [RequestBuilder.Body], [XML],
// [Form], or [Multipart].
//
// Parameters:
//   - value (any): the value marshaled with [encoding/json.Marshal].
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) JSON(value any) (builder *RequestBuilder) {
	b.body = nil
	b.bodyErr = nil
	b.bodyStream = nil
	b.bodyStreamSize = 0
	b.bodyEncoder = func(_ int64) (data []byte, contentType string, err error) {
		data, err = json.Marshal(value)
		contentType = hqgohttpmime.JSON.String()

		return data, contentType, err
	}

	return b
}

// XML encodes value as the XML request body. Encoding happens when the request
// is built, so marshal errors surface from [RequestBuilder.Do]. The
// Content-Type is set to application/xml unless the final request already
// carries one. It replaces any body set by [RequestBuilder.Body], [JSON],
// [Form], or [Multipart].
//
// Parameters:
//   - value (any): the value marshaled with [encoding/xml.Marshal].
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) XML(value any) (builder *RequestBuilder) {
	b.body = nil
	b.bodyErr = nil
	b.bodyStream = nil
	b.bodyStreamSize = 0
	b.bodyEncoder = func(_ int64) (data []byte, contentType string, err error) {
		data, err = xml.Marshal(value)
		contentType = hqgohttpmime.XML.String()

		return data, contentType, err
	}

	return b
}

// Form encodes values as an application/x-www-form-urlencoded request body.
// The Content-Type is set unless the final request already carries one. It
// replaces any body set by [RequestBuilder.Body], [JSON], [XML], or
// [Multipart].
//
// Parameters:
//   - values (url.Values): the form fields.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) Form(values url.Values) (builder *RequestBuilder) {
	b.body = nil
	b.bodyErr = nil
	b.bodyStream = nil
	b.bodyStreamSize = 0
	b.bodyEncoder = func(_ int64) (data []byte, contentType string, err error) {
		return []byte(values.Encode()), "application/x-www-form-urlencoded", nil
	}

	return b
}

// Multipart encodes form as a multipart/form-data request body, reading the
// files when the request is built. The generated Content-Type, including the
// boundary, is used unless the final request already carries one. It replaces
// any body set by [RequestBuilder.Body], [JSON], [XML], or [Form]. Passing nil
// clears the body.
//
// Parameters:
//   - form (*MultipartForm): the fields and files to encode.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) Multipart(form *MultipartForm) (builder *RequestBuilder) {
	b.body = nil
	b.bodyErr = nil
	b.bodyStream = nil
	b.bodyStreamSize = 0

	if form == nil {
		b.bodyEncoder = nil

		return b
	}

	b.bodyEncoder = form.encode

	return b
}

// Context sets the execution context. Do uses this value; nil means
// [context.Background]. It overrides a client default set with [WithContext].
//
// Parameters:
//   - ctx (context.Context): the execution context.
//
// Returns:
//   - builder (*RequestBuilder): the receiver, for chaining.
func (b *RequestBuilder) Context(ctx context.Context) (builder *RequestBuilder) {
	b.ctx = ctx

	return b
}

// Do builds the request and executes it through the client. See [Client.Do]
// for the execution semantics and outcomes. Without a URL it returns
// [ErrNoURL].
//
// Returns:
//   - res (*Response): the final response; the caller owns and must close its
//     body.
//   - err (error): non-nil when building or executing the request fails.
func (b *RequestBuilder) Do() (res *Response, err error) {
	req, err := b.build(b.ctx)
	if err != nil {
		return nil, err
	}

	res, err = b.client.Do(req, b.requestOptions...)

	return res, err
}

// build assembles the request: it resolves the method, merges the query
// parameters, applies the client's default headers (including the default
// User-Agent), then the request's own headers.
//
// Parameters:
//   - ctx (context.Context): the context the request is built with.
//
// Returns:
//   - req (*hqgohttprequest.Request): the built request, with a reusable
//     body.
//   - err (error): non-nil when the URL is missing or invalid or the request
//     cannot be constructed.
func (b *RequestBuilder) build(ctx context.Context) (req *hqgohttprequest.Request, err error) {
	if b.bodyStream != nil {
		defer func() {
			if err != nil {
				if closer, ok := b.bodyStream.(io.Closer); ok {
					_ = closer.Close()
				}
			}
		}()
	}

	rawURL := b.rawURL
	if rawURL == "" {
		err = fmt.Errorf("http.RequestBuilder.Do: %w", ErrNoURL)

		return nil, err
	}

	if b.bodyErr != nil {
		err = fmt.Errorf("http.RequestBuilder.Do: buffering body: %w", b.bodyErr)

		return nil, err
	}

	method := b.method
	if method == "" {
		method = hqgohttpmethod.GET
	}

	body := b.body

	var contentType string

	if b.bodyEncoder != nil {
		limit := b.client.MaxRequestBodySize

		data, encodedContentType, encodeErr := b.bodyEncoder(limit)
		if encodeErr != nil {
			err = fmt.Errorf("http.RequestBuilder.Do: encoding request body: %w", encodeErr)

			return nil, err
		}

		if limit > 0 && int64(len(data)) > limit {
			err = fmt.Errorf("http.RequestBuilder.Do: %w (limit %d bytes)", ErrRequestBodyTooLarge, limit)

			return nil, err
		}

		body = data
		contentType = encodedContentType
	}

	if b.bodyStream != nil {
		if limit := b.client.MaxRequestBodySize; limit > 0 && b.bodyStreamSize > limit {
			err = fmt.Errorf("http.RequestBuilder.Do: %w (limit %d bytes)", ErrRequestBodyTooLarge, limit)

			return nil, err
		}

		req, err = hqgohttprequest.NewStream(ctx, method.String(), rawURL, b.bodyStream, b.bodyStreamSize)
	} else {
		req, err = hqgohttprequest.NewLimited(ctx, method.String(), rawURL, body, b.client.MaxRequestBodySize)
	}

	if err != nil {
		err = fmt.Errorf("http.RequestBuilder.Do: building request: %w", err)

		return nil, err
	}

	if req.URL.Host == "" {
		err = fmt.Errorf("http.RequestBuilder.Do: URL %q has no host", req.URL.Redacted())

		return nil, err
	}

	if len(b.params) > 0 {
		encoded := b.params.Encode()

		switch {
		case req.URL.RawQuery == "":
			req.URL.RawQuery = encoded
		case encoded != "":
			req.URL.RawQuery += "&" + encoded
		}
	}

	for _, h := range b.client.Headers {
		if b.setsHeader(h.Key) {
			continue
		}

		if isHostHeader(h.Key) {
			req.Host = h.Value

			continue
		}

		req.Header.Set(h.Key, h.Value)
	}

	for _, h := range b.headers {
		if isHostHeader(h.key) {
			req.Host = h.value

			continue
		}

		if h.add {
			req.Header.Add(h.key, h.value)
		} else {
			req.Header.Set(h.key, h.value)
		}
	}

	if contentType != "" && req.Header.Get(hqgohttpheader.ContentType.String()) == "" {
		req.Header.Set(hqgohttpheader.ContentType.String(), contentType)
	}

	return req, nil
}

// isHostHeader reports whether key names the Host header, which net/http
// carries in Request.Host rather than Request.Header.
//
// Parameters:
//   - key (string): the header field name, matched case-insensitively.
//
// Returns:
//   - host (bool): true when key is Host.
func isHostHeader(key string) (host bool) {
	host = strings.EqualFold(key, "Host")

	return host
}

// setsHeader reports whether the request replaces a header with
// [RequestBuilder.SetHeader], so a client default must not be applied.
// [RequestBuilder.AddHeader] values append to the client default instead of
// suppressing it.
//
// Parameters:
//   - key (string): the header field name, matched case-insensitively.
//
// Returns:
//   - sets (bool): true when the request replaces the key.
func (b *RequestBuilder) setsHeader(key string) (sets bool) {
	for _, h := range b.headers {
		if !h.add && strings.EqualFold(h.key, key) {
			sets = true

			return sets
		}
	}

	return sets
}

// applyDefaultHeaders fills missing default headers on req. A header already
// present on the request keeps its value, matched case-insensitively. Host
// maps to Request.Host and is applied only when empty.
//
// Parameters:
//   - req (*http.Request): the request to fill.
//   - headers ([]HeaderField): the default headers to apply.
func applyDefaultHeaders(req *http.Request, headers []HeaderField) {
	if len(headers) == 0 {
		return
	}

	if req.Header == nil {
		req.Header = make(http.Header)
	}

	for _, h := range headers {
		if isHostHeader(h.Key) {
			if req.Host == "" {
				req.Host = h.Value
			}

			continue
		}

		if hasHeader(req.Header, h.Key) {
			continue
		}

		req.Header.Set(h.Key, h.Value)
	}
}

// hasHeader reports whether header carries key, matched case-insensitively.
//
// Parameters:
//   - header (http.Header): the header map to inspect.
//   - key (string): the header field name.
//
// Returns:
//   - found (bool): true when the header map contains the key.
func hasHeader(header http.Header, key string) (found bool) {
	for existing := range header {
		if strings.EqualFold(existing, key) {
			found = true

			return found
		}
	}

	return found
}

// MultipartForm collects the fields and files of a multipart/form-data
// request body.
//
// Fields retain insertion order. The encoded body is buffered when the request
// is built, so retries resend the same bytes; the file readers are consumed at
// that point, making a form single-use after a build.
//
// Fields:
//   - fields ([]multipartField): the ordered text fields.
//   - files ([]MultipartFile): the ordered file parts.
type MultipartForm struct {
	fields []multipartField
	files  []MultipartFile
}

// multipartField is one ordered text field of a [MultipartForm].
//
// Fields:
//   - name (string): the field name.
//   - value (string): the field value.
type multipartField struct {
	name  string
	value string
}

// MultipartFile describes one file part of a [MultipartForm].
//
// Fields:
//   - Field (string): the form field name carrying the file.
//   - Filename (string): the filename reported to the server.
//   - Content (io.Reader): the file content, read when the request is built.
type MultipartFile struct {
	Field    string
	Filename string
	Content  io.Reader
}

// Field appends a text field, preserving call order. Repeated calls with the
// same name create repeated parts.
//
// Parameters:
//   - name (string): the form field name.
//   - value (string): the field value.
//
// Returns:
//   - form (*MultipartForm): the receiver, for chaining.
func (f *MultipartForm) Field(name, value string) (form *MultipartForm) {
	f.fields = append(f.fields, multipartField{name: name, value: value})

	return f
}

// File appends a file part. The content is read when the request is built.
//
// Parameters:
//   - field (string): the form field name carrying the file.
//   - filename (string): the filename reported to the server.
//   - content (io.Reader): the file content. Nil is reported as an error when
//     the request is built.
//
// Returns:
//   - form (*MultipartForm): the receiver, for chaining.
func (f *MultipartForm) File(field, filename string, content io.Reader) (form *MultipartForm) {
	f.files = append(f.files, MultipartFile{
		Field:    field,
		Filename: filename,
		Content:  content,
	})

	return f
}

// limitWriter fails once more than limit bytes have been written.
//
// Fields:
//   - w (io.Writer): the destination writer.
//   - limit (int64): the maximum number of bytes to write.
//   - wrote (int64): the number of bytes written so far.
type limitWriter struct {
	w     io.Writer
	limit int64
	wrote int64
}

// Write writes p unless it would exceed the limit.
//
// Parameters:
//   - p ([]byte): the bytes to write.
//
// Returns:
//   - n (int): the number of bytes written.
//   - err (error): an error wrapping [ErrRequestBodyTooLarge] once the limit
//     would be exceeded.
func (l *limitWriter) Write(p []byte) (n int, err error) {
	if int64(len(p)) > l.limit-l.wrote {
		return 0, fmt.Errorf("%w (limit %d bytes)", ErrRequestBodyTooLarge, l.limit)
	}

	n, err = l.w.Write(p)
	l.wrote += int64(n)

	return n, err
}

// encode writes the form into a reusable multipart body. A positive limit stops
// the encoding before more than limit bytes are buffered.
//
// Parameters:
//   - limit (int64): the maximum encoded body size in bytes; non-positive
//     means unlimited.
//
// Returns:
//   - data ([]byte): the encoded body.
//   - contentType (string): the Content-Type including the generated boundary.
//   - err (error): non-nil when a field, file, or the writer fails.
func (f *MultipartForm) encode(limit int64) (data []byte, contentType string, err error) {
	var buffer bytes.Buffer

	var dst io.Writer = &buffer
	if limit > 0 {
		dst = &limitWriter{w: &buffer, limit: limit}
	}

	writer := multipart.NewWriter(dst)

	if werr := f.writeTo(writer); werr != nil {
		return nil, "", werr
	}

	if err = writer.Close(); err != nil {
		return nil, "", fmt.Errorf("http.MultipartForm: closing writer: %w", err)
	}

	return buffer.Bytes(), writer.FormDataContentType(), nil
}

// writeTo writes the form's fields and files to writer. It does not close the
// writer, so the caller can decide how the multipart body ends.
//
// Parameters:
//   - writer (*multipart.Writer): the multipart writer to fill.
//
// Returns:
//   - err (error): non-nil when a field, file, or read fails.
func (f *MultipartForm) writeTo(writer *multipart.Writer) (err error) {
	for _, field := range f.fields {
		if err = writer.WriteField(field.name, field.value); err != nil {
			return fmt.Errorf("http.MultipartForm: writing field %q: %w", field.name, err)
		}
	}

	for _, file := range f.files {
		if file.Content == nil {
			return fmt.Errorf("http.MultipartForm: file %q has a nil reader", file.Filename)
		}

		var part io.Writer

		part, err = writer.CreateFormFile(file.Field, file.Filename)
		if err != nil {
			return fmt.Errorf("http.MultipartForm: creating file part %q: %w", file.Filename, err)
		}

		if _, err = io.Copy(part, file.Content); err != nil {
			return fmt.Errorf("http.MultipartForm: reading file %q: %w", file.Filename, err)
		}
	}

	return nil
}

// Stream returns a reader that produces the multipart body as it is consumed,
// instead of buffering it. Use it with [RequestBuilder.BodyStream] for large
// uploads:
//
//	body, contentType, err := form.Stream()
//	if err != nil {
//		// handle the error
//	}
//
//	res, err := client.Request().
//		Method(method.POST).
//		URL(url).
//		BodyStream(body, -1).
//		SetHeader(header.ContentType.String(), contentType).
//		Do()
//
// The returned reader must be closed by the caller; closing it, or closing the
// request body after a send, unblocks the producer goroutine. A file reader
// that blocks inside Read cannot be interrupted by closing the body; the
// producer goroutine outlives the request until that read returns. A streamed
// body has no replay function, so retries stop with ErrBodyNotReplayable after
// the first attempt. Nil file readers are reported synchronously.
//
// Returns:
//   - body (io.ReadCloser): the streamed multipart body; the caller owns it.
//   - contentType (string): the Content-Type including the generated boundary.
//   - err (error): non-nil when a file reader is nil or the boundary cannot be
//     set.
func (f *MultipartForm) Stream() (body io.ReadCloser, contentType string, err error) {
	for _, file := range f.files {
		if file.Content == nil {
			return nil, "", fmt.Errorf("http.MultipartForm: file %q has a nil reader", file.Filename)
		}
	}

	var preview bytes.Buffer

	probe := multipart.NewWriter(&preview)
	boundary := probe.Boundary()
	contentType = probe.FormDataContentType()

	_ = probe.Close()

	reader, writer := io.Pipe()

	go func() {
		multipartWriter := multipart.NewWriter(writer)

		if serr := multipartWriter.SetBoundary(boundary); serr != nil {
			_ = writer.CloseWithError(serr)

			return
		}

		if werr := f.writeTo(multipartWriter); werr != nil {
			_ = multipartWriter.Close()
			_ = writer.CloseWithError(werr)

			return
		}

		_ = writer.CloseWithError(multipartWriter.Close())
	}()

	body = reader

	return body, contentType, nil
}

// StripSensitiveHeadersOnRedirect returns a [http.Client.CheckRedirect]
// function that removes the given headers when a redirect changes host or
// downgrades from https to http.
//
// net/http already strips Authorization, WWW-Authenticate, Cookie, and
// Cookie2 on a host change; this helper covers application-specific
// credentials such as X-Api-Key. Header names are matched case-insensitively.
//
// Use it with [WithCheckRedirect]:
//
//	client, _ := hqgohttp.New(
//		hqgohttp.WithCheckRedirect(hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key")),
//	)
//
// Parameters:
//   - keys (...string): the header names to strip. Empty keys are ignored.
//
// Returns:
//   - checkRedirect (func(*http.Request, []*http.Request) error): the redirect
//     policy.
func StripSensitiveHeadersOnRedirect(keys ...string) (checkRedirect func(req *http.Request, via []*http.Request) error) {
	checkRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}

		previous := via[len(via)-1]
		if req.URL == nil || previous.URL == nil {
			return nil
		}

		sameHost := strings.EqualFold(req.URL.Host, previous.URL.Host)
		downgrade := previous.URL.Scheme == "https" && req.URL.Scheme == "http"

		if sameHost && !downgrade {
			return nil
		}

		for _, key := range keys {
			if key != "" {
				req.Header.Del(key)
			}
		}

		return nil
	}

	return checkRedirect
}
