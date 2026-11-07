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

func (e *execution) handout(res *http.Response, err error) (*http.Response, error) {
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
