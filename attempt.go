package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// doAttempt runs one attempt: it waits on the rate limiter, arms the
// per-attempt timeout, fires the prepare hook and the [OnRequestFunc]
// callback, performs the round trip (with protocol fallback), records the
// outcome, and asks the retry policy whether to continue. A retryable outcome
// is wrapped in a [retryableError] for the engine; a body that cannot be
// replayed is a final marker.
//
// Parameters:
//   - ctx (context.Context): the execution context.
//
// Returns:
//   - res (*http.Response): the attempt response, or nil.
//   - err (error): nil for a terminal success, the policy's abort error, or a
//     [retryableError] marker wrapping the retryable outcome.
func (e *execution) doAttempt(ctx context.Context) (res *http.Response, err error) {
	e.retryAfter, e.retryAfterSet = 0, false

	if e.opts.RateLimiter != nil {
		if waitErr := e.opts.RateLimiter.Wait(ctx, e.req); waitErr != nil {
			if cause := context.Cause(ctx); cause != nil {
				return nil, cause
			}

			return nil, fmt.Errorf("http.Client.Do: waiting on rate limiter: %w", waitErr)
		}
	}

	e.attempt++
	attemptReq := e.req

	var attemptTimer *time.Timer

	if e.opts.AttemptTimeout > 0 {
		attemptCtx, attemptCancel := context.WithCancelCause(ctx)
		e.attemptCancel = attemptCancel
		attemptReq = e.req.WithContext(attemptCtx)
		attemptTimer = time.AfterFunc(e.opts.AttemptTimeout, func() {
			attemptCancel(context.DeadlineExceeded)
		})
	} else {
		e.attemptCancel = nil
	}

	e.lastAttemptRequest = attemptReq

	if e.attempt > 1 && e.opts.PrepareRetry != nil {
		if prepareErr := e.opts.PrepareRetry(ctx, attemptReq, e.attempt); prepareErr != nil {
			return nil, fmt.Errorf("http.Client.Do: preparing retry attempt %d: %w", e.attempt, prepareErr)
		}
	}

	if e.opts.OnRequest != nil {
		e.opts.OnRequest(RequestEvent{Attempt: e.attempt, Request: attemptReq})
	}

	logHTTPRequest(e.opts.Logger, attemptReq, e.attempt)

	attemptStart := time.Now()
	res, err = e.roundTrip(attemptReq)

	if res != nil {
		e.headersReceived.Store(true)
	}

	if attemptTimer != nil && !attemptTimer.Stop() {
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}

		res = nil
		err = context.DeadlineExceeded
	}

	e.lastAttemptDuration = time.Since(attemptStart)
	e.lastAttemptResponse = res
	e.lastResponseClosed = false
	e.recordAttempt(attemptReq, res, err)

	retry, policyErr := e.opts.RetryPolicy(ctx, attemptReq, res, err, e.attempt)
	if policyErr != nil {
		return res, policyErr
	}

	if retry && e.opts.IdempotentRetries && !isIdempotentMethod(attemptReq.Method) {
		retry = false
	}

	if retry {
		if replayErr := rewindRequestBody(e.req); replayErr != nil {
			if res != nil {
				return res, err
			}

			err = errors.Join(err, replayErr)

			marker := &retryableError{err: err, attempt: e.attempt, final: true}

			return nil, marker
		}
	}

	if !retry {
		return res, err
	}

	if res != nil {
		if wait, ok := retryAfterWait(res); ok {
			e.retryAfter = wait
			e.retryAfterSet = true
		}
	}

	if err == nil {
		if res == nil {
			err = errors.New("http.Client.Do: attempt returned no response and no error")
		} else {
			err = fmt.Errorf("http.Client.Do: retryable HTTP status %s", res.Status)
		}
	}

	return res, &retryableError{err: err, resp: res, attempt: e.attempt}
}

// roundTrip performs the attempt's exchange on the primary client, keeps the
// HTTP/3 failure cache current, and resends on the fallback client when the
// primary failure qualifies and the request may be repeated. A joined error
// reports both failures when the fallback also fails.
//
// Parameters:
//   - req (*http.Request): the attempt request.
//
// Returns:
//   - res (*http.Response): the response, or nil.
//   - err (error): the primary failure, or both failures joined after a
//     failed resend.
func (e *execution) roundTrip(req *http.Request) (res *http.Response, err error) {
	res, err = e.primary.Do(req)

	if e.http3Attempt {
		switch {
		case err == nil && res != nil:
			e.client.clearHTTP3Failure(req.URL.Host)
		case res == nil && err != nil && shouldFallback(true, err):
			e.client.recordHTTP3Failure(req.URL.Host)
		}
	}

	resend := res == nil && err != nil && e.fallback != nil &&
		shouldFallback(e.fallbackOnHTTP3, err) && canResendRequest(req, e.opts.IdempotentRetries)
	if !resend {
		return res, err
	}

	if rewindErr := rewindRequestBody(req); rewindErr != nil {
		return res, errors.Join(err, rewindErr)
	}

	primaryErr := err

	res, err = e.fallback.Do(req)
	if err != nil {
		err = errors.Join(primaryErr, err)
	}

	return res, err
}

// recordAttempt fires the [OnResponseFunc] callback, logs the outcome, and
// appends the attempt to the execution history used by [RetryError].
//
// Parameters:
//   - req (*http.Request): the attempt request.
//   - res (*http.Response): the attempt response, or nil.
//   - err (error): the attempt's transport error, or nil.
func (e *execution) recordAttempt(req *http.Request, res *http.Response, err error) {
	if e.opts.OnResponse != nil {
		e.opts.OnResponse(ResponseEvent{
			Attempt:  e.attempt,
			Request:  req,
			Response: res,
			Err:      err,
			Duration: e.lastAttemptDuration,
		})
	}

	logHTTPResponse(e.opts.Logger, req, res, err, e.attempt, e.lastAttemptDuration)

	record := RetryAttempt{
		Attempt:  e.attempt,
		Err:      err,
		Duration: e.lastAttemptDuration,
	}

	if res != nil {
		record.StatusCode = res.StatusCode
		record.Status = res.Status
	}

	e.history = append(e.history, record)
}
