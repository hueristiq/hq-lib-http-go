package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html/charset"

	hqgohttpstatus "github.com/hueristiq/hq-lib-http-go/status"
)

// responseBody binds a handed-out response body to its request context. It
// stops the body-read timer and cancels the context when the body is closed
// or drained, and surfaces the context's cause when a read fails because the
// context ended.
//
// Fields:
//   - ReadCloser (io.ReadCloser): the underlying response body.
//   - cancel (context.CancelCauseFunc): releases the request context.
//   - attemptCancel (context.CancelCauseFunc): releases the final attempt's
//     context when a per-attempt timeout is configured; nil otherwise.
//   - cause (func() error): reports the request context's cause, if any.
//   - stopCloseWatch (func() bool): stops the client-close watcher; nil when
//     no watcher was registered.
//   - timer (*time.Timer): the optional body-read timeout.
//   - closeOnce (sync.Once): guards the underlying close.
//   - closeErr (error): the underlying close result.
//   - cancelOnce (sync.Once): guards the context cancellations.
//   - closed (atomic.Bool): whether Close has been called, so a read after an
//     explicit close preserves the underlying error instead of reporting the
//     context cancellation that Close triggered.
type responseBody struct {
	io.ReadCloser
	cancel         context.CancelCauseFunc
	attemptCancel  context.CancelCauseFunc
	cause          func() error
	stopCloseWatch func() bool
	timer          *time.Timer
	closeOnce      sync.Once
	closeErr       error
	cancelOnce     sync.Once
	closed         atomic.Bool
}

// Read reads from the underlying body. It stops the body-read timer and
// releases the request context when the body ends or fails. A read error
// caused by the context is replaced with the context's cause so callers see
// context.DeadlineExceeded or context.Canceled rather than a transport
// wrapper. A read after an explicit [responseBody.Close] keeps the underlying
// closed-body error instead of reporting the cancellation that Close triggered.
//
// Parameters:
//   - p ([]byte): the destination buffer.
//
// Returns:
//   - n (int): the number of bytes read.
//   - err (error): io.EOF at the end, the context's cause when the context
//     ended, or the underlying read error.
func (b *responseBody) Read(p []byte) (n int, err error) {
	n, err = b.ReadCloser.Read(p)
	if err == nil {
		return n, nil
	}

	if b.timer != nil {
		b.timer.Stop()
	}

	cause := b.cause()

	b.release()

	if err == io.EOF {
		return n, err
	}

	if b.closed.Load() {
		return n, err
	}

	if cause != nil {
		return n, cause
	}

	return n, err
}

// release cancels the final attempt's context and the request context once.
func (b *responseBody) release() {
	b.cancelOnce.Do(func() {
		if b.attemptCancel != nil {
			b.attemptCancel(nil)
		}

		if b.stopCloseWatch != nil {
			b.stopCloseWatch()
		}

		b.cancel(nil)
	})
}

// Close closes the underlying body, stops the body-read timer, and releases
// the request context.
//
// Returns:
//   - err (error): the underlying close error.
func (b *responseBody) Close() (err error) {
	b.closed.Store(true)

	if b.timer != nil {
		b.timer.Stop()
	}

	// Cancel before closing so a Close that drains an unread body cannot block
	// on a stalled connection.
	b.release()

	b.closeOnce.Do(func() {
		b.closeErr = b.ReadCloser.Close()
	})

	return b.closeErr
}

// drainBody reads and discards up to limit bytes from body and closes it, so
// the underlying connection can be reused for future requests. It reads one
// byte past the limit so a body of exactly limit bytes reaches EOF.
//
// Parameters:
//   - body (io.ReadCloser): the response body to drain.
//   - limit (int64): the maximum number of bytes to read.
func drainBody(body io.ReadCloser, limit int64) {
	if body == nil {
		return
	}

	defer func() {
		_ = body.Close()
	}()

	if limit < math.MaxInt64 {
		limit++
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(body, limit))
}

// snapshotBody reads up to limit bytes from body, closes it, and reports
// whether more data remained. The bytes are retained in a [RetryError] after
// retries are exhausted, so the caller can inspect the server's error payload.
//
// Parameters:
//   - body (io.ReadCloser): the response body to snapshot.
//   - limit (int64): the maximum number of bytes to copy.
//
// Returns:
//   - data ([]byte): the copied prefix, at most limit bytes.
//   - truncated (bool): true when the body held more than limit bytes.
//   - readErr (error): a read error encountered while copying, or nil.
func snapshotBody(body io.ReadCloser, limit int64) (data []byte, truncated bool, readErr error) {
	if body == nil || limit <= 0 {
		if body != nil {
			_ = body.Close()
		}

		return nil, false, nil
	}

	defer func() {
		_ = body.Close()
	}()

	if limit == math.MaxInt64 {
		data, readErr = io.ReadAll(body)

		return data, false, readErr
	}

	data, readErr = io.ReadAll(io.LimitReader(body, limit+1))

	if int64(len(data)) > limit {
		truncated = true

		data = data[:limit]
	}

	return data, truncated, readErr
}

// Response wraps the final [http.Response] returned by an execution and adds
// status classification and an enhanced [Body].
//
// The embedded [http.Response] stays accessible through [Response.Std], so
// fields such as StatusCode, Header, Request, and TLS keep their standard
// meanings. The raw body remains at res.Std().Body; res.Body is the enhanced
// wrapper. Do not read both. The embedded Close field is the standard-library
// flag, not a method; close the body with res.Body.Close().
//
// A Response is not safe for concurrent use, matching [http.Response].
//
// Fields:
//   - Response (*http.Response): the embedded standard-library response.
//   - Body (*Body): the enhanced body wrapper. The raw body remains at
//     Response.Response.Body; read only one of the two.
type Response struct {
	*http.Response

	Body *Body
}

// newResponse wraps a raw response and its body.
//
// Parameters:
//   - res (*http.Response): the response to wrap.
//   - maxSize (int64): the maximum number of body bytes the helpers buffer;
//     non-positive means unlimited.
//
// Returns:
//   - response (*Response): the wrapper.
func newResponse(res *http.Response, maxSize int64) (response *Response) {
	response = &Response{Response: res}

	contentType := ""

	if res != nil && res.Header != nil {
		contentType = res.Header.Get("Content-Type")
	}

	var reader io.ReadCloser

	if res != nil {
		reader = res.Body
	}

	response.Body = newBody(reader, contentType, maxSize)

	return response
}

// Std returns the underlying standard-library response.
//
// Returns:
//   - response (*http.Response): the embedded response, or nil for a nil
//     wrapper.
func (r *Response) Std() (response *http.Response) {
	if r == nil {
		return nil
	}

	return r.Response
}

// IsInformational reports whether the status code is in the 1xx range.
//
// Returns:
//   - informational (bool): true for 1xx responses.
func (r *Response) IsInformational() (informational bool) {
	return r.status().IsInformational()
}

// IsSuccess reports whether the status code is in the 2xx range.
//
// Returns:
//   - success (bool): true for 2xx responses.
func (r *Response) IsSuccess() (success bool) {
	return r.status().IsSuccess()
}

// IsRedirection reports whether the status code is in the 3xx range.
//
// Returns:
//   - redirection (bool): true for 3xx responses.
func (r *Response) IsRedirection() (redirection bool) {
	return r.status().IsRedirection()
}

// IsClientError reports whether the status code is in the 4xx range.
//
// Returns:
//   - clientError (bool): true for 4xx responses.
func (r *Response) IsClientError() (clientError bool) {
	return r.status().IsClientError()
}

// IsServerError reports whether the status code is in the 5xx range.
//
// Returns:
//   - serverError (bool): true for 5xx responses.
func (r *Response) IsServerError() (serverError bool) {
	return r.status().IsServerError()
}

// IsError reports whether the status code is a client or server error.
//
// Returns:
//   - isError (bool): true for 4xx and 5xx responses.
func (r *Response) IsError() (isError bool) {
	return r.status().IsError()
}

// status returns the embedded response's status for classification. A nil
// wrapper or response yields the zero status.
//
// Returns:
//   - status (hqgohttpstatus.Status): the response status code.
func (r *Response) status() (status hqgohttpstatus.Status) {
	if r == nil || r.Response == nil {
		return 0
	}

	return hqgohttpstatus.Status(r.StatusCode)
}

// Body is the enhanced response body. It caches the content on first read and
// exposes decoding and streaming helpers.
//
// Fields:
//   - reader (io.ReadCloser): the live body, replaced by a cached reader after
//     the first buffered read.
//   - contentType (string): the response Content-Type, used by [Body.UTF8].
//   - maxSize (int64): the cap on bytes buffered by [Body.Bytes]; non-positive
//     means unlimited.
//   - cacheOnce (sync.Once): guards the buffered read.
//   - content ([]byte): the cached bytes.
//   - cacheErr (error): the cached read error.
//   - closeOnce (sync.Once): guards the underlying close.
//   - closeErr (error): the underlying close result.
//
// Bytes, String, JSON, XML, and UTF8 read the body once, close it, and reuse
// the cached content. Stream returns a [StreamReader] over the live reader, or
// over the cache when the body has already been read. A Body is not safe for
// concurrent use.
type Body struct {
	reader      io.ReadCloser
	contentType string
	maxSize     int64
	cacheOnce   sync.Once
	content     []byte
	cacheErr    error
	closeOnce   sync.Once
	closeErr    error
}

// newBody wraps a response body.
//
// Parameters:
//   - reader (io.ReadCloser): the raw body.
//   - contentType (string): the response Content-Type, used by [Body.UTF8].
//   - maxSize (int64): the maximum number of bytes [Body.Bytes] buffers;
//     non-positive means unlimited.
//
// Returns:
//   - body (*Body): the wrapper.
func newBody(reader io.ReadCloser, contentType string, maxSize int64) (body *Body) {
	body = &Body{
		reader:      reader,
		contentType: contentType,
		maxSize:     maxSize,
	}

	return body
}

// Compile-time guards ensuring the body types satisfy the standard interfaces.
var (
	_ io.ReadCloser = (*Body)(nil)
	_ io.ReadCloser = (*StreamReader)(nil)
)

// Read delegates to the underlying reader, or to the cached content once
// [Body.Bytes] has read it. It lets *Body satisfy [io.Reader].
//
// Read consumes the live body. If it runs before [Body.Bytes], [Body.String],
// [Body.JSON], [Body.XML], or [Body.UTF8], those helpers cache only the unread
// remainder. Call one of them first when the whole body is needed both ways.
//
// Parameters:
//   - p ([]byte): the destination buffer.
//
// Returns:
//   - n (int): the number of bytes read.
//   - err (error): io.EOF at the end of the body, or the read error.
func (b *Body) Read(p []byte) (n int, err error) {
	if b == nil || b.reader == nil {
		return 0, io.EOF
	}

	return b.reader.Read(p)
}

// Bytes returns the body content, reading and closing the underlying body on
// first use.
//
// The content is cached, so repeated calls return the same bytes even after
// the body has been consumed. The returned slice is owned by the Body and must
// not be modified. Any bytes already consumed through [Body.Read] are not part
// of the cache; read through Bytes first when the full body is needed.
//
// When a [WithMaxResponseBodySize] cap is configured and exceeded, the returned
// prefix holds the bytes read up to the limit and err wraps
// [ErrResponseBodyTooLarge].
//
// Returns:
//   - data ([]byte): the cached body, or nil when the response has no body.
//   - err (error): a body-read or close error, or the error from the first
//     read on later calls.
func (b *Body) Bytes() (data []byte, err error) {
	if b == nil {
		return nil, errors.New("http.Body.Bytes: body is nil")
	}

	b.cacheOnce.Do(func() {
		b.content, b.cacheErr = b.read()
	})

	return b.content, b.cacheErr
}

// read consumes the underlying reader once and caches the result.
//
// Returns:
//   - data ([]byte): the bytes read.
//   - err (error): the read or close error.
func (b *Body) read() (data []byte, err error) {
	if b.reader == nil {
		return nil, nil
	}

	if b.maxSize > 0 && b.maxSize != math.MaxInt64 {
		data, err = io.ReadAll(io.LimitReader(b.reader, b.maxSize+1))
		if err == nil && int64(len(data)) > b.maxSize {
			data = data[:b.maxSize]
			err = fmt.Errorf("http.Body.Bytes: %w (limit %d bytes)", ErrResponseBodyTooLarge, b.maxSize)
		}
	} else {
		data, err = io.ReadAll(b.reader)
	}

	if closeErr := b.Close(); err == nil {
		err = closeErr
	}

	b.reader = io.NopCloser(bytes.NewReader(data))

	return data, err
}

// String returns the body content as a string, using the [Body.Bytes] cache.
//
// Returns:
//   - text (string): the body text.
//   - err (error): a body-read error.
func (b *Body) String() (text string, err error) {
	data, err := b.Bytes()
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// JSON decodes the body content as JSON into v, using the [Body.Bytes] cache.
//
// Parameters:
//   - v (any): the destination passed to [encoding/json.Unmarshal].
//
// Returns:
//   - err (error): a body-read or JSON-decoding error.
func (b *Body) JSON(v any) (err error) {
	data, err := b.Bytes()
	if err != nil {
		return fmt.Errorf("http.Body.JSON: reading body: %w", err)
	}

	if err = json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("http.Body.JSON: decoding body: %w", err)
	}

	return nil
}

// XML decodes the body content as XML into v, using the [Body.Bytes] cache. An
// [encoding/xml.Decoder] converts the declared document encoding to UTF-8, so
// non-UTF-8 XML is decoded too.
//
// Parameters:
//   - v (any): the destination decoded from the body.
//
// Returns:
//   - err (error): a body-read or XML-decoding error.
func (b *Body) XML(v any) (err error) {
	data, err := b.Bytes()
	if err != nil {
		return fmt.Errorf("http.Body.XML: reading body: %w", err)
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel

	if err = decoder.Decode(v); err != nil {
		return fmt.Errorf("http.Body.XML: decoding body: %w", err)
	}

	return nil
}

// UTF8 decodes the body content to UTF-8 using the response Content-Type,
// falling back to charset detection when the header is absent or unknown. It
// uses the [Body.Bytes] cache. An empty body returns an empty string with no
// error.
//
// Returns:
//   - text (string): the decoded body text.
//   - err (error): a body-read or charset-decoding error.
func (b *Body) UTF8() (text string, err error) {
	data, err := b.Bytes()
	if err != nil {
		return "", fmt.Errorf("http.Body.UTF8: reading body: %w", err)
	}

	if len(data) == 0 {
		return "", nil
	}

	reader, err := charset.NewReader(bytes.NewReader(data), b.contentType)
	if err != nil {
		return "", fmt.Errorf("http.Body.UTF8: detecting charset: %w", err)
	}

	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("http.Body.UTF8: decoding body: %w", err)
	}

	return string(decoded), nil
}

// Stream returns a buffered reader over the body. If the body has already been
// read and cached, the reader serves the cached content; otherwise it streams
// the live reader. The caller owns the returned reader and must close it.
//
// Call Stream once, and do not mix it with Read, Bytes, String, JSON, XML,
// UTF8, or SSE on the same Body: buffering consumes the live reader.
//
// Returns:
//   - stream (*StreamReader): the buffered body reader, or nil when the body
//     has no reader.
func (b *Body) Stream() (stream *StreamReader) {
	if b == nil || b.reader == nil {
		return nil
	}

	stream = &StreamReader{
		Reader: bufio.NewReader(b.reader),
		body:   b,
	}

	return stream
}

// SSE reads Server-Sent Events from the body and calls fn for each event. It
// closes the stream when it finishes. Returning false from fn stops reading
// and returns nil; a nil handler is an error.
//
// Lines are split on "\n" with an optional trailing "\r"; a CR-only stream is
// not recognized, and a single line longer than 1 MiB aborts the stream.
//
// Parameters:
//   - fn (func(event *SSEEvent) bool): the per-event callback.
//
// Returns:
//   - err (error): a read or parser error, or a nil-handler error.
func (b *Body) SSE(fn func(event *SSEEvent) bool) (err error) {
	if fn == nil {
		return errors.New("http.Body.SSE: nil handler")
	}

	stream := b.Stream()
	if stream == nil {
		return errors.New("http.Body.SSE: body is nil")
	}

	defer func() {
		_ = stream.Close()
	}()

	return parseSSE(stream, fn)
}

// Close closes the underlying body once. It is safe to call more than once.
//
// Returns:
//   - err (error): the body's close error.
func (b *Body) Close() (err error) {
	if b == nil || b.reader == nil {
		return nil
	}

	b.closeOnce.Do(func() {
		b.closeErr = b.reader.Close()
	})

	return b.closeErr
}

// StreamReader wraps a buffered body reader and closes the owning [Body].
//
// Fields:
//   - Reader (*bufio.Reader): the buffered reader.
//   - body (*Body): the owning body, closed by [StreamReader.Close].
type StreamReader struct {
	*bufio.Reader

	body *Body
}

// Close closes the underlying body.
//
// Returns:
//   - err (error): the body's close error.
func (s *StreamReader) Close() (err error) {
	if s == nil || s.body == nil {
		return nil
	}

	return s.body.Close()
}

// SSEEvent is one Server-Sent Event.
//
// Fields:
//   - Event (string): the event type; empty means "message".
//   - Data (string): the event data, with multiple data lines joined by "\n".
//   - ID (string): the last event ID.
//   - Retry (time.Duration): the reconnection delay from a retry field.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
	Retry time.Duration
}

// parseSSE reads Server-Sent Events from reader and dispatches them to fn.
//
// Parameters:
//   - reader (io.Reader): the event stream.
//   - fn (func(event *SSEEvent) bool): the per-event callback; false stops
//     reading.
//
// Returns:
//   - err (error): a scanner or read error.
func parseSSE(reader io.Reader, fn func(event *SSEEvent) bool) (err error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		event   SSEEvent
		data    []string
		hasData bool
	)

	reset := func() {
		event.Event = ""
		data = nil
		hasData = false
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")

		if line == "" {
			if hasData {
				current := event
				current.Data = strings.Join(data, "\n")

				if !fn(&current) {
					return nil
				}
			}

			reset()

			continue
		}

		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		}

		switch field {
		case "data":
			data = append(data, value)
			hasData = true
		case "event":
			event.Event = value
		case "id":
			if !strings.ContainsRune(value, '\x00') {
				event.ID = value
			}
		case "retry":
			if milliseconds, convErr := strconv.Atoi(value); convErr == nil && milliseconds >= 0 {
				event.Retry = time.Duration(milliseconds) * time.Millisecond
			}
		}
	}

	if err = scanner.Err(); err != nil {
		return fmt.Errorf("http.Body.SSE: reading event stream: %w", err)
	}

	return nil
}
