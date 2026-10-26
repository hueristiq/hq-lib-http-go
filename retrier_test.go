package http_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgohttpstatus "github.com/hueristiq/hq-lib-http-go/status"
)

// testRetrier is a minimal retry engine implementing [hqgohttp.Retrier]. It
// retries errors approved by options.RetryOn, sleeps for a configurable wait,
// and optionally honors options.RetryAfter.
//
// Fields:
//   - maxAttempts (int): maximum attempts; non-positive means 3.
//   - wait (time.Duration): delay between attempts; non-positive means 1 ms.
//   - honorRetryAfter (bool): whether to prefer the parsed Retry-After wait.
type testRetrier struct {
	maxAttempts     int
	wait            time.Duration
	honorRetryAfter bool
}

// Retry implements [hqgohttp.Retrier].
func (r testRetrier) Retry(ctx context.Context, operation hqgohttp.RetryOperation, options hqgohttp.RetrierOptions) (res *http.Response, err error) {
	maxAttempts := r.maxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	wait := r.wait

	for attempt := 1; ; attempt++ {
		if cause := context.Cause(ctx); cause != nil {
			return res, cause
		}

		res, err = operation()
		if err == nil {
			return res, nil
		}

		if options.RetryOn != nil && !options.RetryOn(err) {
			return res, err
		}

		if attempt >= maxAttempts {
			return res, err
		}

		if r.honorRetryAfter {
			if retryAfter, ok := options.RetryAfter(); ok {
				wait = retryAfter
			}
		}

		if wait <= 0 {
			wait = time.Millisecond
		}

		if options.OnRetry != nil {
			options.OnRetry(hqgohttp.RetrierEvent{Attempt: attempt, Err: err, Wait: wait})
		}

		timer := time.NewTimer(wait)

		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()

			return res, context.Cause(ctx)
		}
	}
}

func TestRetrierFunc(t *testing.T) {
	t.Parallel()

	var _ hqgohttp.Retrier = hqgohttp.RetrierFunc(nil)

	type contextKey struct{}

	ctx := context.WithValue(t.Context(), contextKey{}, "marker")

	wantRes := &http.Response{StatusCode: http.StatusTeapot, Body: http.NoBody}
	wantErr := errors.New("retrier failed")

	var gotValue any

	retrier := hqgohttp.RetrierFunc(func(gotCtx context.Context, operation hqgohttp.RetryOperation, _ hqgohttp.RetrierOptions) (*http.Response, error) {
		gotValue = gotCtx.Value(contextKey{})

		res, err := operation()
		require.NoError(t, err)
		assert.Same(t, wantRes, res)

		return wantRes, wantErr
	})

	res, err := retrier.Retry(ctx, func() (*http.Response, error) { //nolint:bodyclose // The sentinel response carries http.NoBody.
		return wantRes, nil
	}, hqgohttp.RetrierOptions{})

	require.ErrorIs(t, err, wantErr)
	assert.Same(t, wantRes, res)
	assert.Equal(t, "marker", gotValue, "Expected the retrier to receive the execution context")
}

func TestClient_NoRetrierMakesSingleAttempt(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)

		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.Error(t, err)
	assert.Nil(t, res)
	assert.Equal(t, int32(1), attempts.Load(), "Expected a single attempt without a retrier")

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, 1, retryErr.Attempts, "Expected the single attempt to be classified as a RetryError")
}

func TestClient_WithRetrier(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		_, _ = w.Write([]byte("recovered"))
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}))

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	assert.Equal(t, "recovered", readBody(t, res))
	assert.Equal(t, int32(2), attempts.Load(), "Expected the engine to drive both attempts")
}

func TestClient_WithRequestRetrier(t *testing.T) {
	t.Parallel()

	newUnavailableServer := func(t *testing.T) (serverURL string, attempts *atomic.Int32) {
		t.Helper()

		attempts = &atomic.Int32{}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)

			w.WriteHeader(http.StatusServiceUnavailable)
		}))

		t.Cleanup(server.Close)

		serverURL = server.URL

		return
	}

	t.Run("per-request engine wins", func(t *testing.T) {
		t.Parallel()

		serverURL, attempts := newUnavailableServer(t)

		client := hqgohttp.New(hqgohttp.WithRetrier(testRetrier{maxAttempts: 1}))

		res, err := client.Get(serverURL, hqgohttp.WithRequestRetrier(testRetrier{maxAttempts: 3}))

		require.Error(t, err)
		assert.Nil(t, res)
		assert.Equal(t, int32(3), attempts.Load(), "Expected the per-request engine to run three attempts")
	})

	t.Run("nil disables retries", func(t *testing.T) {
		t.Parallel()

		serverURL, attempts := newUnavailableServer(t)

		client := hqgohttp.New(hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}))

		res, err := client.Get(serverURL, hqgohttp.WithRequestRetrier(nil))

		require.Error(t, err)
		assert.Nil(t, res)
		assert.Equal(t, int32(1), attempts.Load(), "Expected a nil per-request retrier to disable retries")
	})
}

// retryAfterRetrier records the Retry-After value reported by the client and
// performs a single attempt.
type retryAfterRetrier struct {
	wait time.Duration
	ok   bool
}

// Retry implements [hqgohttp.Retrier].
func (r *retryAfterRetrier) Retry(_ context.Context, operation hqgohttp.RetryOperation, options hqgohttp.RetrierOptions) (res *http.Response, err error) {
	res, err = operation()

	r.wait, r.ok = options.RetryAfter()

	return res, err
}

func TestClient_RetrierOptions_RetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		retryAfter string
		wantWait   time.Duration
		wantOK     bool
	}{
		{"seconds", "10", 10 * time.Second, true},
		{"zero", "0", 0, true},
		{"absent", "", 0, false},
		{"invalid", "later", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}

				w.WriteHeader(http.StatusServiceUnavailable)
			}))

			t.Cleanup(server.Close)

			retrier := &retryAfterRetrier{}

			res, err := hqgohttp.New(hqgohttp.WithRetrier(retrier)).Get(server.URL)

			require.Error(t, err)
			assert.Nil(t, res)
			assert.Equal(t, tt.wantWait, retrier.wait, "Expected the parsed Retry-After wait")
			assert.Equal(t, tt.wantOK, retrier.ok, "Expected the header's presence to be reported")
		})
	}
}

func TestDefaultRetryPolicy_ResponseWithErrorIsFinal(t *testing.T) {
	t.Parallel()

	policyErr := errors.New("redirect refused")

	resp := &http.Response{StatusCode: http.StatusInternalServerError}

	retry, rerr := hqgohttp.DefaultRetryPolicy(t.Context(), nil, resp, policyErr, 1)

	assert.False(t, retry)
	require.NoError(t, rerr)

	retry, rerr = hqgohttp.DefaultRetryPolicy(t.Context(), nil, nil, policyErr, 1)

	assert.True(t, retry, "Expected a bare transport error to remain retryable")
	require.NoError(t, rerr)
}

func TestDefaultRetryPolicy_ReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	cancel()

	retry, rerr := hqgohttp.DefaultRetryPolicy(ctx, nil, nil, fmt.Errorf("wrapped: %w", context.Canceled), 1)

	assert.False(t, retry)
	assert.ErrorIs(t, rerr, context.Canceled)
}

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()

	errConnection := errors.New("connection refused")

	tests := []struct {
		name      string
		ctx       func(t *testing.T) context.Context
		resp      *http.Response
		err       error
		wantRetry bool
		wantErr   bool
	}{
		{
			name: "connection error is retried",
			err: &url.Error{
				Op:  "Get",
				URL: "http://example.com",
				Err: errConnection,
			},
			wantRetry: true,
		},
		{
			name:      "bare transport error is retried",
			err:       errConnection,
			wantRetry: true,
		},
		{
			name: "canceled context is not retried",
			ctx: func(t *testing.T) context.Context {
				t.Helper()

				ctx, cancel := context.WithCancel(t.Context())

				cancel()

				return ctx
			},
			err:       context.Canceled,
			wantRetry: false,
			wantErr:   true,
		},
		{
			name: "exhausted redirect limit is not retried",
			err: &url.Error{
				Op:  "Get",
				URL: "http://example.com",
				Err: errors.New("stopped after 10 redirects"),
			},
			wantRetry: false,
		},
		{
			name: "unsupported scheme is not retried",
			err: &url.Error{
				Op:  "Get",
				URL: "gopher://example.com",
				Err: errors.New(`unsupported protocol scheme "gopher"`),
			},
			wantRetry: false,
		},
		{
			name: "unknown authority is not retried",
			err: &url.Error{
				Op:  "Get",
				URL: "https://example.com",
				Err: x509.UnknownAuthorityError{},
			},
			wantRetry: false,
		},
		{
			name: "certificate verification failure is not retried",
			err: &url.Error{
				Op:  "Get",
				URL: "https://example.com",
				Err: &tls.CertificateVerificationError{},
			},
			wantRetry: false,
		},
		{
			name:      "429 is retried",
			resp:      &http.Response{StatusCode: hqgohttpstatus.TooManyRequests.Int()},
			wantRetry: true,
		},
		{
			name:      "408 is retried",
			resp:      &http.Response{StatusCode: hqgohttpstatus.RequestTimeout.Int()},
			wantRetry: true,
		},
		{
			name: "nxdomain is not retried",
			err: &url.Error{
				Op:  "Get",
				URL: "https://example.com",
				Err: &net.DNSError{Err: "no such host", Name: "example.com", IsNotFound: true},
			},
			wantRetry: false,
		},
		{
			name: "dns timeout is retried",
			err: &url.Error{
				Op:  "Get",
				URL: "https://example.com",
				Err: &net.DNSError{Err: "i/o timeout", Name: "example.com", IsTimeout: true},
			},
			wantRetry: true,
		},
		{
			name:      "500 is retried",
			resp:      &http.Response{StatusCode: hqgohttpstatus.InternalServerError.Int()},
			wantRetry: true,
		},
		{
			name:      "503 is retried",
			resp:      &http.Response{StatusCode: hqgohttpstatus.ServiceUnavailable.Int()},
			wantRetry: true,
		},
		{
			name:      "501 is not retried",
			resp:      &http.Response{StatusCode: hqgohttpstatus.NotImplemented.Int()},
			wantRetry: false,
		},
		{
			name:      "zero status is retried",
			resp:      &http.Response{StatusCode: 0},
			wantRetry: true,
		},
		{
			name:      "200 is not retried",
			resp:      &http.Response{StatusCode: hqgohttpstatus.OK.Int()},
			wantRetry: false,
		},
		{
			name:      "404 is not retried",
			resp:      &http.Response{StatusCode: hqgohttpstatus.NotFound.Int()},
			wantRetry: false,
		},
		{
			name:      "nil response and nil error is not retried",
			wantRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			if tt.ctx != nil {
				ctx = tt.ctx(t)
			}

			retry, rerr := hqgohttp.DefaultRetryPolicy(ctx, nil, tt.resp, tt.err, 1)

			assert.Equal(t, tt.wantRetry, retry)

			if tt.wantErr {
				require.Error(t, rerr)
			} else {
				assert.NoError(t, rerr)
			}
		})
	}
}

func TestRetryError_IsRetriesExhausted(t *testing.T) {
	t.Parallel()

	retryErr := &hqgohttp.RetryError{
		Attempts: 1,
		Err:      errors.New("last failure"),
	}

	require.ErrorIs(t, retryErr, hqgohttp.ErrRetriesExhausted)
	require.NotErrorIs(t, retryErr, context.Canceled)
}

func TestClient_RetryError_RequestBodyLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "0123456789")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
		hqgohttp.WithRetryErrorBodyLimit(64),
	)

	res, err := client.Get(server.URL, hqgohttp.WithRequestRetryErrorBodyLimit(4))

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, "0123", string(retryErr.Body))
	assert.True(t, retryErr.BodyTruncated)
}

func TestClient_RetryError_History(t *testing.T) {
	t.Parallel()

	statuses := []int{
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
		http.StatusInternalServerError,
	}

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(attempts.Add(1)) - 1
		if index >= len(statuses) {
			index = len(statuses) - 1
		}

		w.WriteHeader(statuses[index])
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
	)

	res, err := client.Get(server.URL)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	require.Len(t, retryErr.History, len(statuses))

	for i, record := range retryErr.History {
		assert.Equal(t, i+1, record.Attempt)
		assert.Equal(t, statuses[i], record.StatusCode)
		assert.NotEmpty(t, record.Status)
		require.NoError(t, record.Err)
		assert.Greater(t, record.Duration, time.Duration(0))
	}

	assert.Error(t, retryErr.Err)
}

func TestClient_RetryError_TransportFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	serverURL := server.URL

	server.Close()

	client := hqgohttp.New(hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}))

	res, err := client.Get(serverURL)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, 2, retryErr.Attempts)
	assert.Zero(t, retryErr.StatusCode)
	assert.Empty(t, retryErr.Status)
	assert.Error(t, retryErr.Err)
}

func TestClient_RetryError_NoRetries(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	res, err := client.Get(server.URL)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, 1, retryErr.Attempts)
	assert.Equal(t, http.StatusInternalServerError, retryErr.StatusCode)
}

func TestClient_RetryError_BodyLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "0123456789")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
		hqgohttp.WithRetryErrorBodyLimit(4),
	)

	res, err := client.Get(server.URL)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, "0123", string(retryErr.Body))
	assert.True(t, retryErr.BodyTruncated)
}

func TestClient_RetryError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Error", "yes")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
		hqgohttp.WithRetryErrorBodyLimit(hqgohttp.RecommendedRetryErrorBodyLimit),
	)

	res, err := client.Get(server.URL)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.Error(t, err)
	assert.Nil(t, res)

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, 2, retryErr.Attempts)
	assert.Equal(t, http.MethodGet, retryErr.Method)
	assert.Equal(t, http.StatusInternalServerError, retryErr.StatusCode)
	assert.Equal(t, "500 Internal Server Error", retryErr.Status)
	assert.Equal(t, "yes", retryErr.Header.Get("X-Error"))
	assert.Contains(t, string(retryErr.Body), "boom")
	assert.False(t, retryErr.BodyTruncated)
	assert.Error(t, retryErr.Err)
}

func TestClient_RetryError_BodyDisabledByDefault(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "sensitive payload")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	res, err := client.Get(server.URL)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Empty(t, retryErr.Body)
	assert.False(t, retryErr.BodyTruncated)
}

func TestRetryError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()

	var nilErr *hqgohttp.RetryError

	assert.Equal(t, "http.Client.Do: giving up", nilErr.Error())
	require.NoError(t, nilErr.Unwrap())

	cause := errors.New("cause")
	plain := &hqgohttp.RetryError{
		Attempts: 2,
		Method:   http.MethodGet,
		URL:      "http://example.com",
		Err:      cause,
	}

	assert.Contains(t, plain.Error(), "giving up after 2 attempt(s)")
	require.ErrorIs(t, plain, cause)

	status := &hqgohttp.RetryError{
		Attempts:   1,
		Method:     http.MethodGet,
		URL:        "http://example.com",
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
	}

	assert.Contains(t, status.Error(), "500 Internal Server Error")
}

func TestRetrierFunc_Nil(t *testing.T) {
	t.Parallel()

	var retrier hqgohttp.RetrierFunc

	res, err := retrier.Retry(t.Context(), nil, hqgohttp.RetrierOptions{})

	require.Error(t, err)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "nil retrier")
}
