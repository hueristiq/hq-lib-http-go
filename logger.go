package http

import (
	"net/http"
	"time"
)

// Logger defines the contract for the structured logging surface used by a
// [Client].
//
// Implementations provide the four leveled methods of a structured logger,
// accepting a message and an alternating sequence of key/value pairs. The
// interface is satisfied by [log/slog.Logger] and by the standard
// [log.Logger] adapters that expose the same shape, so applications can plug
// their existing logger without an adapter of their own. The client never
// inspects the logger beyond calling these methods; which levels it emits at
// is part of the client's documented behavior, not of this contract.
//
// A nil Logger is valid and means logging is disabled: the client checks for
// nil before every call, so implementations do not need a nil-receiver path
// and tests can pass nil to silence output. Implementations shared across
// clients or goroutines must be safe for concurrent use, because the client
// logs from the goroutine running each attempt and from the retry hook.
// [WithLogger] and [WithRequestLogger] install one at client and request scope
// respectively.
type Logger interface {
	// Debug logs a diagnostic event, such as the outgoing request line and
	// the attempt outcome.
	//
	// Parameters:
	//   - msg (string): the message text.
	//   - args (...any): alternating keys and values attached to the record.
	Debug(msg string, args ...any)

	// Info logs an informational event.
	//
	// Parameters:
	//   - msg (string): the message text.
	//   - args (...any): alternating keys and values attached to the record.
	Info(msg string, args ...any)

	// Warn logs a recoverable problem, such as an attempt that will be
	// retried.
	//
	// Parameters:
	//   - msg (string): the message text.
	//   - args (...any): alternating keys and values attached to the record.
	Warn(msg string, args ...any)

	// Error logs a terminal failure that ends an execution.
	//
	// Parameters:
	//   - msg (string): the message text.
	//   - args (...any): alternating keys and values attached to the record.
	Error(msg string, args ...any)
}

// WithLogger returns a [ClientOptionFunc] that sets the client's logger. Nil
// disables logging.
//
// Parameters:
//   - logger (Logger): the logger, or nil to disable logging.
//
// Returns:
//   - f (ClientOptionFunc): a functional option that sets the logger.
func WithLogger(logger Logger) (f ClientOptionFunc) {
	return func(opts *ClientOptions) {
		opts.Logger = logger
	}
}

// WithRequestLogger returns a [RequestOptionFunc] that overrides the client's
// logger for one execution. Nil disables logging for that request.
//
// Parameters:
//   - logger (Logger): the logger, or nil to disable logging for the request.
//
// Returns:
//   - f (RequestOptionFunc): a functional option that sets the logger.
func WithRequestLogger(logger Logger) (f RequestOptionFunc) {
	return func(opts *RequestOptions) {
		opts.Logger = logger
	}
}

// logHTTPRequest records an outgoing attempt.
//
// Parameters:
//   - logger (Logger): the logger, or nil.
//   - req (*http.Request): the attempt request.
//   - attempt (int): the 1-based attempt number.
func logHTTPRequest(logger Logger, req *http.Request, attempt int) {
	if logger == nil {
		return
	}

	logger.Debug("http request", "method", requestMethod(req), "url", requestURL(req), "attempt", attempt)
}

// logHTTPResponse records an attempt's outcome.
//
// Parameters:
//   - logger (Logger): the logger, or nil.
//   - req (*http.Request): the attempt request.
//   - res (*http.Response): the attempt response, or nil.
//   - err (error): the attempt error, or nil.
//   - attempt (int): the 1-based attempt number.
//   - duration (time.Duration): the attempt's elapsed time.
func logHTTPResponse(logger Logger, req *http.Request, res *http.Response, err error, attempt int, duration time.Duration) {
	if logger == nil {
		return
	}

	args := []any{
		"method", requestMethod(req),
		"url", requestURL(req),
		"attempt", attempt,
		"duration", duration,
	}

	if res != nil {
		args = append(args, "status", res.StatusCode)
	}

	if err != nil {
		args = append(args, "error", err)
	}

	logger.Debug("http response", args...)
}

// logHTTPRetry records an approved retry and its wait.
//
// Parameters:
//   - logger (Logger): the logger, or nil.
//   - req (*http.Request): the failed attempt request.
//   - res (*http.Response): the failed attempt response, or nil.
//   - err (error): the failure that triggered the retry.
//   - attempt (int): the 1-based number of the failed attempt.
//   - wait (time.Duration): the delay before the next attempt.
//   - duration (time.Duration): the failed attempt's elapsed time.
func logHTTPRetry(logger Logger, req *http.Request, res *http.Response, err error, attempt int, wait, duration time.Duration) {
	if logger == nil {
		return
	}

	args := []any{
		"method", requestMethod(req),
		"url", requestURL(req),
		"attempt", attempt,
		"wait", wait,
		"duration", duration,
	}

	if res != nil {
		args = append(args, "status", res.StatusCode)
	}

	if err != nil {
		args = append(args, "error", err)
	}

	logger.Warn("http retry", args...)
}

// logHTTPFailure records the terminal outcome of an execution.
//
// Parameters:
//   - logger (Logger): the logger, or nil.
//   - req (*http.Request): the request that failed.
//   - attempts (int): the number of attempts made.
//   - err (error): the terminal error.
func logHTTPFailure(logger Logger, req *http.Request, attempts int, err error) {
	if logger == nil || err == nil {
		return
	}

	logger.Error("http request failed", "method", requestMethod(req), "url", requestURL(req), "attempts", attempts, "error", err)
}

// requestMethod returns req's method, or an empty string when req is nil.
//
// Parameters:
//   - req (*http.Request): the request.
//
// Returns:
//   - method (string): the request method.
func requestMethod(req *http.Request) (method string) {
	if req == nil {
		return ""
	}

	return req.Method
}

// requestURL returns req's redacted URL, or an empty string when req or its URL
// is nil.
//
// Parameters:
//   - req (*http.Request): the request.
//
// Returns:
//   - url (string): the redacted URL.
func requestURL(req *http.Request) (url string) {
	if req == nil || req.URL == nil {
		return ""
	}

	return req.URL.Redacted()
}
