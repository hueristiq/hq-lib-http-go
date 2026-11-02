package http

import (
	"net/http"
	"time"
)

// Logger is the minimal logging surface used by the client. It is satisfied by
// [log/slog.Logger]. A nil Logger disables logging.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
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
	return func(opts *clientOptions) {
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
	return func(opts *requestOptions) {
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
