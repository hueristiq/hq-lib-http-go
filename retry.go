package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
	hqgoretrierbackoff "github.com/hueristiq/hq-lib-retrier-go/backoff"
)

// isMarkedRetryable reports whether err carries the retryable marker and is not
// final, i.e. whether the retry engine should schedule another attempt. Errors
// without the marker, and final markers, are returned to the caller unchanged.
//
// Parameters:
//   - err (error): the failure to classify.
//
// Returns:
//   - retry (bool): true when another attempt may follow.
func isMarkedRetryable(err error) (retry bool) {
	var marker *retryableError

	if !errors.As(err, &marker) {
		return false
	}

	retry = !marker.final

	return retry
}

// isIdempotentMethod reports whether method is idempotent. The client uses it
// when [WithRetryIdempotent] is enabled. GET, HEAD, OPTIONS, TRACE, PUT, and
// DELETE are idempotent per RFC 9110; QUERY is safe and idempotent per RFC
// 10008.
//
// Parameters:
//   - method (string): the HTTP method.
//
// Returns:
//   - idempotent (bool): true for GET, HEAD, OPTIONS, TRACE, PUT, DELETE, and
//     QUERY.
func isIdempotentMethod(method string) (idempotent bool) {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace,
		http.MethodPut, http.MethodDelete, hqgohttpmethod.QUERY.String():
		idempotent = true
	default:
		idempotent = false
	}

	return idempotent
}

// isHTTP1BrokenTransportError reports whether err is the failure net/http
// produces when a cleartext HTTP/1.x exchange receives an HTTP/2 frame: the
// server speaks HTTP/2 without an upgrade. When it fires, the attempt is
// retried on the HTTP/2 fallback client.
//
// net/http builds this error as
// fmt.Errorf("net/http: HTTP/1.x transport connection broken: %w", err)
// wrapping an unexported badStringError, so no errors.As target exists;
// matching the message is the only way to detect it. The check is isolated
// here so the string match lives in exactly one place.
//
// Parameters:
//   - err (error): the error to inspect.
//
// Returns:
//   - broken (bool): true when the error indicates a broken HTTP/1.x
//     transport connection.
func isHTTP1BrokenTransportError(err error) (broken bool) {
	if err == nil {
		return false
	}

	message := err.Error()

	broken = strings.Contains(message, `malformed HTTP version "HTTP/2"`) ||
		strings.Contains(message, "malformed HTTP response")

	return broken
}

// canReplayRequestBody reports whether req's body can be sent again, without
// performing the rewind. A reusable body is always replayable; any other body
// needs GetBody.
//
// Parameters:
//   - req (*http.Request): the request to inspect.
//
// Returns:
//   - replayable (bool): true when the body can be prepared for another send.
func canReplayRequestBody(req *http.Request) (replayable bool) {
	if req.Body == nil || req.Body == http.NoBody {
		return true
	}

	if _, isReusable := req.Body.(*hqgohttprequest.ReusableReadCloser); isReusable {
		return true
	}

	replayable = req.GetBody != nil

	return replayable
}

// canResendRequest reports whether the internal protocol fallback may resend
// req. When idempotentOnly is set, only idempotent methods qualify; the body
// must also be replayable.
//
// Parameters:
//   - req (*http.Request): the request to inspect.
//   - idempotentOnly (bool): whether non-idempotent methods are excluded.
//
// Returns:
//   - resendable (bool): true when the request may be sent again.
func canResendRequest(req *http.Request, idempotentOnly bool) (resendable bool) {
	if idempotentOnly && !isIdempotentMethod(req.Method) {
		return false
	}

	resendable = canReplayRequestBody(req)

	return resendable
}

// resetReusableRequestBody rewinds a reusable request body to the start when
// the request carries one. Other body types are left untouched, since the
// first attempt owns their initial position.
//
// Parameters:
//   - req (*http.Request): the request whose body is prepared for its first
//     attempt.
func resetReusableRequestBody(req *http.Request) {
	if reusable, isReusable := req.Body.(*hqgohttprequest.ReusableReadCloser); isReusable && reusable != nil {
		reusable.Reset()
	}
}

// rewindRequestBody makes req's body resendable after an attempt, so a retry
// or the HTTP/2 fallback client can send the request again.
//
// A body with nothing to rewind — nil or [http.NoBody] — needs no work. A
// [*hqgohttprequest.ReusableReadCloser] body is rewound in place. Any other
// body is refetched through GetBody; without GetBody, or when GetBody fails,
// the error wraps [ErrBodyNotReplayable].
//
// Parameters:
//   - req (*http.Request): the request whose body is prepared for resending.
//
// Returns:
//   - err (error): nil when the body can be sent again; an error wrapping
//     [ErrBodyNotReplayable] otherwise.
func rewindRequestBody(req *http.Request) (err error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}

	if reusable, isReusable := req.Body.(*hqgohttprequest.ReusableReadCloser); isReusable {
		if reusable != nil {
			reusable.Reset()
		}

		return nil
	}

	if req.GetBody == nil {
		return ErrBodyNotReplayable
	}

	body, gerr := req.GetBody()
	if gerr != nil {
		return fmt.Errorf("%w: %w", ErrBodyNotReplayable, gerr)
	}

	req.Body = body

	return nil
}

// Backoff is the delay strategy used between retry attempts. It receives the
// 1-based number of the failed attempt and returns the delay before the next
// one.
type Backoff = hqgoretrierbackoff.Backoff

// RetryPolicy decides whether a request is retried after an attempt.
//
// It is invoked after every attempt with the attempt's outcome: the overall
// execution context, the attempt request, the response (nil when the transport
// failed before any response arrived), the error (nil when the round trip
// itself succeeded), and the 1-based attempt number.
//
// Returning retry as false ends the execution: the response and error are
// returned to the caller as they are. Returning a non-nil rerr also ends the
// execution, but the response and rerr are returned instead — use it to abort
// retries on errors the caller must see unchanged, such as context
// cancellation.
//
// When a response is retried, the client drains and closes its body so the
// connection can be reused. When a policy stops retries on a response, the
// response body is handed to the caller unclosed; the caller owns it.
//
// A response paired with an error is a terminal outcome; [DefaultRetryPolicy]
// returns false for it. A policy may still choose to retry it, but the client
// then treats the response as retryable and drains it.
//
// A policy is invoked synchronously on the goroutine running the attempt and
// must be safe for concurrent use across requests. A panicking policy
// propagates its panic to the caller of the entry point.
//
// Parameters:
//   - ctx (context.Context): the overall execution context, carrying
//     cancellation signals and deadlines.
//   - req (*http.Request): the attempt request. Its context may carry a
//     per-attempt deadline when [WithAttemptTimeout] is set.
//   - resp (*http.Response): the response received on the attempt, or nil.
//   - err (error): the transport error of the attempt, or nil.
//   - attempt (int): the 1-based attempt number.
//
// Returns:
//   - retry (bool): true to schedule another attempt, false to stop.
//   - rerr (error): an error replacing the attempt's outcome; non-nil stops
//     retries even when retry is true.
type RetryPolicy func(ctx context.Context, req *http.Request, resp *http.Response, err error, attempt int) (retry bool, rerr error)

var (
	// redirectsErrorRegex matches the error net/http produces when the
	// configured redirect limit is exhausted. The error is not typed
	// specifically, so matching on the message is the only way to detect it;
	// the check is isolated here so the string match lives in exactly one
	// place.
	redirectsErrorRegex = regexp.MustCompile(`stopped after \d+ redirects\z`)

	// schemeErrorRegex matches the error net/http produces for an unsupported
	// protocol scheme. The error is not typed specifically, so matching on the
	// message is the only way to detect it; the check is isolated here so the
	// string match lives in exactly one place.
	schemeErrorRegex = regexp.MustCompile(`unsupported protocol scheme`)
)

// DefaultRetryPolicy is the default [RetryPolicy]: it retries connection and
// transport errors, 408 Request Timeout, 429 Too Many Requests, and 5xx
// responses (except 501 Not Implemented), and it never retries a canceled
// context, a response paired with an error, an exhausted redirect limit, an
// unsupported protocol scheme, a TLS certificate verification failure, or a
// permanent DNS resolution failure such as NXDOMAIN.
//
// It is the policy applied when neither [WithRetryPolicy] nor
// [WithRequestRetryPolicy] is supplied.
//
// The policy does not consider HTTP method idempotency: the client enforces
// [WithRetryIdempotent], which is enabled by default, so POST and PATCH are
// not retried unless the caller opts out.
//
// Parameters:
//   - ctx (context.Context): the overall execution context, carrying
//     cancellation signals and deadlines.
//   - _ (*http.Request): accepted for the [RetryPolicy] signature; the default
//     policy does not inspect the request.
//   - resp (*http.Response): the response received on the attempt, or nil.
//   - err (error): the transport error of the attempt, or nil.
//   - _ (int): accepted for the [RetryPolicy] signature; the default policy
//     does not inspect the attempt number.
//
// Returns:
//   - retry (bool): true when another attempt is worthwhile.
//   - rerr (error): the context's cause when the context is done; nil
//     otherwise.
func DefaultRetryPolicy(ctx context.Context, _ *http.Request, resp *http.Response, err error, _ int) (retry bool, rerr error) {
	// Do not retry on context.Canceled or context.DeadlineExceeded: the caller
	// is no longer waiting for an answer. Use the cause so a deadline enforced
	// by the client's execution timer surfaces as context.DeadlineExceeded.
	if cause := context.Cause(ctx); cause != nil {
		rerr = cause

		return false, rerr
	}

	// A response paired with an error is a terminal outcome: net/http returns
	// this shape for a redirect-policy abort, and retrying would repeat the
	// abort the caller asked for.
	if resp != nil && err != nil {
		return false, nil
	}

	if err != nil {
		// Retrying cannot fix an exhausted redirect chain, an unsupported
		// scheme, an untrusted certificate, or a name that does not resolve.
		if isPermanentTransportError(err) {
			return false, nil
		}

		// The error is likely recoverable — connection refused, reset,
		// timeout, DNS hiccup — so retry.
		retry = true

		return retry, nil
	}

	if resp != nil {
		// 429 Too Many Requests and 408 Request Timeout are recoverable.
		// Servers may add a Retry-After header, which the retry engine honors
		// (capped at the maximum retry wait).
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
			retry = true

			return retry, nil
		}

		// Retry on 500-range responses (except 501 Not Implemented, which
		// retrying cannot fix) to give the server time to recover. The range
		// check also catches invalid codes such as 0.
		if resp.StatusCode == 0 || (resp.StatusCode >= 500 && resp.StatusCode != http.StatusNotImplemented) {
			retry = true

			return retry, nil
		}
	}

	return false, nil
}

// isCertificateError reports whether err wraps a TLS certificate verification
// failure. Unlike the redirect-limit and unsupported-scheme failures, these
// errors are typed, so they are matched with errors.As.
//
// Parameters:
//   - err (error): the error to inspect.
//
// Returns:
//   - ok (bool): true when err is a certificate verification failure.
func isCertificateError(err error) (ok bool) {
	var unknownAuthorityError x509.UnknownAuthorityError

	var hostnameError x509.HostnameError

	var certificateInvalidError x509.CertificateInvalidError

	var certificateVerificationError *tls.CertificateVerificationError

	ok = errors.As(err, &unknownAuthorityError) ||
		errors.As(err, &hostnameError) ||
		errors.As(err, &certificateInvalidError) ||
		errors.As(err, &certificateVerificationError)

	return ok
}

// isPermanentTransportError reports whether err describes a failure that
// another attempt cannot fix. Retrying an exhausted redirect chain, an
// unsupported protocol scheme, an untrusted certificate, or a name that does
// not resolve only repeats the same failure.
//
// Context cancellation and deadline errors are deliberately not classified
// here: the overall execution context is checked separately by
// [DefaultRetryPolicy], while a per-attempt timeout must remain retryable.
//
// Parameters:
//   - err (error): the error to inspect.
//
// Returns:
//   - permanent (bool): true when another attempt cannot succeed.
func isPermanentTransportError(err error) (permanent bool) {
	if err == nil {
		return false
	}

	if redirectsErrorRegex.MatchString(err.Error()) || schemeErrorRegex.MatchString(err.Error()) {
		return true
	}

	if isCertificateError(err) {
		return true
	}

	var dnsError *net.DNSError
	if errors.As(err, &dnsError) && dnsError.IsNotFound {
		return true
	}

	return false
}

// DefaultRetryErrorBodyLimit is zero: a [RetryError] does not carry a copy of
// the final response body unless body capture is explicitly enabled with
// [WithRetryErrorBodyLimit].
const DefaultRetryErrorBodyLimit int64 = 0

// RecommendedRetryErrorBodyLimit is the conventional 64 KiB bound for a
// [RetryError] body snapshot. It is a suggested value for
// [WithRetryErrorBodyLimit], not a default: body capture is opt-in.
const RecommendedRetryErrorBodyLimit int64 = 64 << 10

// RetryError reports a retry failure: either the retry budget was exhausted or
// the request body could not be replayed. It carries the last attempt's HTTP
// status and headers, and — when body capture is enabled with
// [WithRetryErrorBodyLimit] — a bounded copy of its body, plus the underlying
// error. When the last attempt failed before receiving a response, StatusCode
// is zero and only Err is set.
//
// Body capture is disabled by default because response payloads can contain
// credentials, session tokens, or personal data. When enabled, treat
// [RetryError.Body] and [RetryError.Header] as sensitive and redact them before
// logging.
//
// Use errors.As to retrieve it:
//
//	var retryErr *RetryError
//	if errors.As(err, &retryErr) {
//		fmt.Println(retryErr.StatusCode, string(retryErr.Body))
//	}
//
// It matches [ErrRetriesExhausted] when the retry budget was exhausted.
//
// Fields:
//   - Attempts (int): the number of attempts made, including the first one.
//   - Method (string): the request method.
//   - URL (string): the request URL with credentials redacted.
//   - StatusCode (int): the last response's status code, or zero.
//   - Status (string): the last response's status line, or empty.
//   - Header (http.Header): a clone of the last response's headers, or nil.
//   - Body ([]byte): up to the configured retry-error body limit of the last
//     response body; nil unless body capture is enabled. The snapshot may be
//     truncated.
//   - BodyTruncated (bool): whether Body holds only a prefix of the response.
//   - BodyErr (error): a read error encountered while capturing the body
//     snapshot, or nil.
//   - Err (error): the last attempt's failure. For an outcome stopped by a
//     body that cannot be replayed, it wraps [ErrBodyNotReplayable].
//   - History ([]RetryAttempt): every attempt in order, including the last one.
//
// A RetryError matches [ErrRetriesExhausted] with errors.Is only when the
// retry budget was actually exhausted. An outcome stopped because the request
// body could not be replayed is final: it matches [ErrBodyNotReplayable]
// instead.
type RetryError struct {
	Attempts      int
	Method        string
	URL           string
	StatusCode    int
	Status        string
	Header        http.Header
	Body          []byte
	BodyTruncated bool
	BodyErr       error
	Err           error
	History       []RetryAttempt

	final bool
}

// RetryAttempt describes one attempt recorded in a [RetryError].
//
// Fields:
//   - Attempt (int): the 1-based attempt number.
//   - StatusCode (int): the response status code, or zero when the attempt
//     failed before a response arrived.
//   - Status (string): the response status line, or empty.
//   - Err (error): the transport error for the attempt; nil for a status-only
//     failure, whose status is in StatusCode.
//   - Duration (time.Duration): the attempt's elapsed time, including any
//     internal protocol fallback.
type RetryAttempt struct {
	Attempt    int
	StatusCode int
	Status     string
	Err        error
	Duration   time.Duration
}

// Error returns the retry failure's message.
//
// Returns:
//   - msg (string): the error message.
func (e *RetryError) Error() (msg string) {
	if e == nil {
		return "http.Client.Do: giving up"
	}

	if e.StatusCode != 0 {
		msg = fmt.Sprintf("http.Client.Do: giving up after %d attempt(s) (%s %s): %s",
			e.Attempts, e.Method, e.URL, e.Status)

		if e.final && e.Err != nil {
			msg += ": " + e.Err.Error()
		}

		return msg
	}

	if e.Err != nil {
		msg = fmt.Sprintf("http.Client.Do: giving up after %d attempt(s) (%s %s): %v",
			e.Attempts, e.Method, e.URL, e.Err)

		return msg
	}

	msg = fmt.Sprintf("http.Client.Do: giving up after %d attempt(s) (%s %s)",
		e.Attempts, e.Method, e.URL)

	return msg
}

// Unwrap exposes the last attempt's failure for errors.Is and errors.As.
//
// Returns:
//   - err (error): the last attempt's failure.
func (e *RetryError) Unwrap() (err error) {
	if e == nil {
		return nil
	}

	err = e.Err

	return err
}

// Is reports whether target is [ErrRetriesExhausted], so callers can classify
// an exhaustion with errors.Is without depending on the error message. An
// outcome stopped by a body that could not be replayed is final and does not
// match the sentinel; it matches [ErrBodyNotReplayable] through [RetryError.Unwrap].
//
// Parameters:
//   - target (error): the sentinel to compare.
//
// Returns:
//   - match (bool): true when target is [ErrRetriesExhausted] and the retry
//     budget was exhausted.
func (e *RetryError) Is(target error) (match bool) {
	if target != ErrRetriesExhausted {
		return false
	}

	if e == nil {
		return true
	}

	match = !e.final

	return match
}

// retryableError marks an attempt's failure as worth another attempt. The
// retry engine retries only errors carrying this marker — an unwrapped error
// from the attempt operation is final and returned to the caller unchanged —
// and [Client.Do] unwraps the marker when the attempts run out, so the caller
// never sees it.
//
// Fields:
//   - err (error): the attempt's failure: its transport error, or a status
//     error describing the rejected response.
//   - resp (*http.Response): the final response, when one was received. Its
//     body has been drained or snapshotted and closed.
//   - body ([]byte): a bounded copy of the final response body.
//   - bodyTruncated (bool): whether body holds only a prefix.
//   - bodyErr (error): a read error from capturing the body snapshot.
//   - attempt (int): the 1-based attempt number.
//   - final (bool): whether the outcome is terminal even though it carries the
//     marker, such as a body that cannot be replayed. The retry engine does not
//     schedule another attempt for a final marker, and [Client.Do] still turns
//     it into a [RetryError].
type retryableError struct {
	err           error
	resp          *http.Response
	body          []byte
	bodyTruncated bool
	bodyErr       error
	attempt       int
	final         bool
}

// Error returns the wrapped failure's message.
//
// Returns:
//   - msg (string): the error message.
func (e *retryableError) Error() (msg string) {
	msg = e.err.Error()

	return msg
}

// Unwrap exposes the wrapped failure for errors.Is and errors.As.
//
// Returns:
//   - err (error): the wrapped failure.
func (e *retryableError) Unwrap() (err error) {
	err = e.err

	return err
}

// retryAfterWait parses the Retry-After header of resp, following RFC 9110:
// either a number of seconds or an HTTP-date. Surrounding whitespace is
// ignored, and the obsolete RFC 850 and asctime date forms are accepted. A
// seconds value too large for a time.Duration is clamped, and a value of zero
// or a date in the past is honored as an immediate retry.
//
// Examples:
//
//	Retry-After: 120
//	Retry-After: Fri, 31 Dec 1999 23:59:59 GMT
//
// Parameters:
//   - resp (*http.Response): the response whose header is parsed.
//
// Returns:
//   - wait (time.Duration): the requested wait; 0 for a date in the past.
//   - ok (bool): true when the header was present and parseable.
func retryAfterWait(resp *http.Response) (wait time.Duration, ok bool) {
	header := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if header == "" {
		return 0, false
	}

	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil {
		// A negative delay makes no sense; leave the header unhonored.
		if seconds < 0 {
			return 0, false
		}

		// Clamp before converting so a huge value cannot overflow into a
		// negative duration. The retry engine caps the wait at its maximum
		// anyway; this keeps the parsed value well-defined.
		if maxSeconds := int64(math.MaxInt64) / int64(time.Second); seconds > maxSeconds {
			seconds = maxSeconds
		}

		wait = time.Duration(seconds) * time.Second
		ok = true

		return wait, ok
	}

	// http.ParseTime accepts IMF-fixdate plus the obsolete RFC 850 and
	// asctime forms that RFC 9110 permits recipients to understand. Fall back
	// to the broader RFC 1123 parser for zones such as UTC that
	// http.ParseTime rejects even though servers emit them.
	retryTime, err := http.ParseTime(header)
	if err != nil {
		retryTime, err = time.Parse(time.RFC1123, header)
	}

	if err != nil {
		return 0, false
	}

	if until := time.Until(retryTime); until > 0 {
		wait = until
	}

	ok = true

	return wait, ok
}
