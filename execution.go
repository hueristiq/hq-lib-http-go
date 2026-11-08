package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	hqgoretrier "github.com/hueristiq/hq-lib-retrier-go"
	hqgoretrierbackoff "github.com/hueristiq/hq-lib-retrier-go/backoff"
)

// execution carries one [Client.Do] call from validation through retries to
// the handed-out response. It owns a cancellable context derived from the
// request context — and, independently, from the client lifetime — that lives
// until the caller closes the response body.
//
// Fields:
//   - client (*Client): the owning client.
//   - req (*http.Request): the cloned request, with default headers applied.
//   - opts (*RequestOptions): the effective execution options.
//   - primary (*http.Client): the client attempts run on first.
//   - fallback (*http.Client): the client a failed primary attempt may resend
//     on; nil when the protocol mode has no fallback.
//   - fallbackOnHTTP3 (bool): whether a primary HTTP/3 failure may resend on
//     the fallback client.
//   - http3Attempt (bool): whether the current attempt speaks HTTP/3, for
//     failure-cache bookkeeping.
//   - ctx (context.Context): the execution context; canceled when the
//     response body closes, the client closes, or a timeout fires.
//   - headersReceived (atomic.Bool): set once any response arrives; gates the
//     execution timeout, which bounds only the wait for headers.
//   - cancel (context.CancelCauseFunc): cancels ctx with a cause.
//   - stopCloseWatch (func() bool): detaches the client-close watcher; a
//     no-op stub until [Client.newExecution] installs the real one.
//   - execTimer (*time.Timer): the execution-timeout timer, or nil.
//   - attemptCancel (context.CancelCauseFunc): cancels the current attempt's
//     per-attempt context, or nil without [WithAttemptTimeout].
//   - attempt (int): the 1-based number of the current attempt.
//   - lastAttemptRequest (*http.Request): the request the last attempt sent.
//   - lastAttemptResponse (*http.Response): the response the last attempt
//     received, or nil.
//   - lastAttemptDuration (time.Duration): the last attempt's elapsed time.
//   - lastResponseClosed (bool): whether the last response body is drained
//     and closed.
//   - history ([]RetryAttempt): per-attempt records for [RetryError].
//   - retryAfter (time.Duration): a server-supplied Retry-After wait honored
//     by the next backoff.
//   - retryAfterSet (bool): whether retryAfter carries a value.
type execution struct {
	client   *Client
	req      *http.Request
	opts     *RequestOptions
	primary  *http.Client
	fallback *http.Client

	fallbackOnHTTP3 bool
	http3Attempt    bool
	ctx             context.Context //nolint:containedctx // One execution owns this context until its body closes.
	headersReceived atomic.Bool
	cancel          context.CancelCauseFunc
	stopCloseWatch  func() bool
	execTimer       *time.Timer
	attemptCancel   context.CancelCauseFunc

	attempt             int
	lastAttemptRequest  *http.Request
	lastAttemptResponse *http.Response
	lastAttemptDuration time.Duration
	lastResponseClosed  bool
	history             []RetryAttempt
	retryAfter          time.Duration
	retryAfterSet       bool
}

// do is [Client.Do] after option resolution: validate, build the execution,
// and run it. A nil response releases the execution immediately; a handed-out
// response body releases it on close.
//
// Parameters:
//   - req (*http.Request): the caller's request.
//   - opts (*RequestOptions): the effective execution options.
//
// Returns:
//   - res (*http.Response): the final response, owned by the caller.
//   - err (error): non-nil on validation or terminal execution failure.
func (c *Client) do(req *http.Request, opts *RequestOptions) (res *http.Response, err error) {
	if err := c.validateRequest(req); err != nil {
		return nil, err
	}

	exec, err := c.newExecution(req, opts)
	if err != nil {
		return nil, err
	}

	defer func() {
		if res == nil {
			exec.release()
		}

		if err != nil {
			logHTTPFailure(opts.Logger, req, exec.attempt, err)
		}
	}()

	return exec.run()
}

// newExecution assembles the execution for one [Client.Do] call: it clones
// the request onto a fresh cancel-cause context, applies the default headers,
// wires the client-close watcher and the execution timer, and selects the
// primary and fallback clients. On selection failure the partially built
// execution is released.
//
// Parameters:
//   - req (*http.Request): the caller's request.
//   - opts (*RequestOptions): the effective execution options.
//
// Returns:
//   - exec (*execution): the assembled execution.
//   - err (error): non-nil when client selection fails (proxy resolution, or
//     an unsupported proxied HTTP/3 request).
func (c *Client) newExecution(req *http.Request, opts *RequestOptions) (exec *execution, err error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	exec = &execution{
		client:          c,
		opts:            opts,
		primary:         c.config.HTTPClient,
		fallback:        c.fallbackHTTPClient,
		fallbackOnHTTP3: c.fallbackOnHTTP3,
		ctx:             ctx,
		cancel:          cancel,
		stopCloseWatch:  func() bool { return true },
	}
	exec.req = req.Clone(ctx)
	exec.req.Header = req.Header.Clone()
	applyDefaultHeaders(exec.req, c.config.Headers)

	if c.closeCtx != nil {
		exec.stopCloseWatch = context.AfterFunc(c.closeCtx, func() { //nolint:contextcheck // The client lifetime is an independent parent of every execution.
			cancel(ErrClientClosed)
		})
	}

	if c.config.Timeout > 0 {
		exec.execTimer = time.AfterFunc(c.config.Timeout, func() {
			if !exec.headersReceived.Load() {
				cancel(context.DeadlineExceeded)
			}
		})
	}

	if err := exec.selectClients(); err != nil {
		exec.release()

		return nil, err
	}

	return exec, nil
}

// selectClients resolves which clients the execution uses. A proxied
// HTTP/3-primary request either fails with [ErrHTTP3ProxyUnsupported] or
// downgrades to the fallback client, and a host in the HTTP/3 failure cache
// skips HTTP/3 for this execution.
//
// Returns:
//   - err (error): non-nil when the proxy cannot be resolved or HTTP/3 cannot
//     honor the proxy.
func (e *execution) selectClients() (err error) {
	if e.client.http3Primary {
		proxyURL, err := e.client.proxyFor(e.req)
		if err != nil {
			return fmt.Errorf("http.Client.Do: resolving proxy: %w", err)
		}

		if proxyURL != nil {
			if !e.fallbackOnHTTP3 {
				return fmt.Errorf("http.Client.Do: %w", ErrHTTP3ProxyUnsupported)
			}

			e.primary, e.fallback = e.client.fallbackHTTPClient, nil
			e.fallbackOnHTTP3 = false
		}
	}

	e.http3Attempt = e.fallbackOnHTTP3
	if e.http3Attempt && e.client.http3RecentlyFailed(e.req.URL.Host) {
		e.primary, e.fallback = e.client.fallbackHTTPClient, nil
		e.fallbackOnHTTP3 = false
		e.http3Attempt = false
	}

	return nil
}

// run drives the retry engine over [execution.doAttempt] and folds the
// outcome into the [Client.Do] contract: a retryable response surfaces as the
// final response, a final marker becomes a [*RetryError], and a canceled
// execution context reports its cause. The wait between attempts prefers a
// server-supplied Retry-After value over the exponential backoff, capped at
// retryWaitMax.
//
// Returns:
//   - res (*http.Response): the final response, or nil.
//   - err (error): non-nil on terminal failure.
func (e *execution) run() (res *http.Response, err error) {
	retryAfterBackoff := func(minDelay, maxDelay time.Duration) (backoff hqgoretrierbackoff.Backoff) {
		base := hqgoretrierbackoff.ExponentialWithEqualJitter(minDelay, maxDelay)

		return func(attempt int) (wait time.Duration) {
			if e.retryAfterSet {
				wait = min(e.retryAfter, maxDelay)
				e.retryAfterSet = false

				return wait
			}

			return base(attempt)
		}
	}

	res, err = hqgoretrier.RetryWithData(e.ctx, e.doAttempt,
		hqgoretrier.WithMaxAttempts(e.opts.RetryMaxAttempts),
		hqgoretrier.WithWaitMin(retryWaitMin),
		hqgoretrier.WithWaitMax(retryWaitMax),
		hqgoretrier.WithBackoff(retryAfterBackoff),
		hqgoretrier.WithRetryOn(isMarkedRetryable),
		hqgoretrier.WithOnRetry(e.onRetry),
	)

	if res == nil && err == nil {
		err = errors.New("http.Client.Do: retrier returned no response and no error")
	}

	if e.execTimer != nil && !e.execTimer.Stop() && !e.headersReceived.Load() {
		e.cancel(context.DeadlineExceeded)
	}

	e.client.closeIdleConnectionsIfDue()

	if e.ctx.Err() != nil && res == nil {
		err = context.Cause(e.ctx)
	} else {
		var marker *retryableError
		if errors.As(err, &marker) {
			if marker.resp != nil {
				res = marker.resp
				err = nil
			} else {
				res = nil
				err = e.newRetryError(marker)
			}
		}
	}

	return e.handout(res, err)
}

// newRetryError builds the [*RetryError] reported when attempts are
// exhausted, snapshotting the attempt history so later mutation cannot alias
// it.
//
// Parameters:
//   - marker (*retryableError): the terminal retry marker.
//
// Returns:
//   - err (*RetryError): the exhaustion error.
func (e *execution) newRetryError(marker *retryableError) (err *RetryError) {
	return &RetryError{
		Attempts: marker.attempt,
		Method:   e.req.Method,
		URL:      e.req.URL.Redacted(),
		Err:      marker.err,
		History:  append([]RetryAttempt(nil), e.history...),
		final:    marker.final,
	}
}

// onRetry is the retry engine's hook between attempts: it drains and closes
// a retried response body so its connection can be reused, cancels the
// attempt context, and emits the log line and the [OnRetryFunc] callback.
//
// Parameters:
//   - event (hqgoretrier.RetryEvent): the engine's retry descriptor, carrying
//     the wrapped attempt failure and the scheduled wait.
func (e *execution) onRetry(event hqgoretrier.RetryEvent) {
	attemptErr := event.Err

	var marker *retryableError
	if errors.As(attemptErr, &marker) {
		if marker.resp != nil && marker.resp.Body != nil {
			drainBody(marker.resp.Body, e.opts.ResponseReadLimit)

			if marker.resp == e.lastAttemptResponse {
				e.lastResponseClosed = true
			}
		}

		attemptErr = marker.err
	}

	if e.attemptCancel != nil {
		e.attemptCancel(nil)
	}

	logHTTPRetry(e.opts.Logger, e.lastAttemptRequest, e.lastAttemptResponse, attemptErr, event.Attempt, event.Wait, e.lastAttemptDuration)

	if e.opts.OnRetry != nil {
		e.opts.OnRetry(RetryEvent{
			Attempt:  event.Attempt,
			Request:  e.lastAttemptRequest,
			Response: e.lastAttemptResponse,
			Err:      attemptErr,
			Wait:     event.Wait,
			Duration: e.lastAttemptDuration,
		})
	}
}

// handout prepares the final outcome for the caller. A nil response closes a
// stranded last-attempt body; a response with a body is wrapped in a
// [responseBody] so its close — or the body-read timeout — releases the
// execution.
//
// Parameters:
//   - res (*http.Response): the final response, or nil.
//   - err (error): the final error, or nil.
//
// Returns:
//   - out (*http.Response): the response, body-wrapped when it carries one.
//   - outErr (error): the final error.
func (e *execution) handout(res *http.Response, err error) (out *http.Response, outErr error) {
	if res == nil {
		if !e.lastResponseClosed && e.lastAttemptResponse != nil && e.lastAttemptResponse.Body != nil {
			_ = e.lastAttemptResponse.Body.Close()
			e.lastResponseClosed = true
		}

		return nil, err
	}

	if res.Body == nil || res.Body == http.NoBody {
		e.release()

		return res, err
	}

	var bodyTimer *time.Timer
	if e.client.config.ResponseBodyTimeout > 0 {
		bodyTimer = time.AfterFunc(e.client.config.ResponseBodyTimeout, func() {
			e.cancel(context.DeadlineExceeded)
		})
	}

	res.Body = &responseBody{
		ReadCloser:     res.Body,
		cancel:         e.cancel,
		attemptCancel:  e.attemptCancel,
		stopCloseWatch: e.stopCloseWatch,
		cause: func() error {
			return context.Cause(e.ctx)
		},
		timer: bodyTimer,
	}

	return res, err
}

// release frees the execution's resources: it stops the execution timer,
// cancels any live attempt context, detaches the client-close watcher, and
// cancels the execution context. It is idempotent by construction — each
// guarded component tolerates repeated calls.
func (e *execution) release() {
	if e.execTimer != nil {
		e.execTimer.Stop()
	}

	if e.attemptCancel != nil {
		e.attemptCancel(nil)
	}

	if e.stopCloseWatch != nil {
		e.stopCloseWatch()
	}

	if e.cancel != nil {
		e.cancel(nil)
	}
}
