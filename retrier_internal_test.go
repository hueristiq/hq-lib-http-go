package http

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
)

// internalTestRetrier is a minimal [Retrier] for tests in this package. It
// retries errors approved by options.RetryOn with a fixed wait.
//
// Fields:
//   - maxAttempts (int): maximum attempts; non-positive means 3.
//   - wait (time.Duration): delay between attempts; non-positive means 1 ms.
type internalTestRetrier struct {
	maxAttempts int
	wait        time.Duration
}

// Retry implements [Retrier].
func (r internalTestRetrier) Retry(ctx context.Context, operation RetryOperation, options RetrierOptions) (res *http.Response, err error) {
	maxAttempts := r.maxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	wait := r.wait
	if wait <= 0 {
		wait = time.Millisecond
	}

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

		if options.OnRetry != nil {
			options.OnRetry(RetrierEvent{Attempt: attempt, Err: err, Wait: wait})
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

func TestRetryAfterWait(t *testing.T) {
	t.Parallel()

	futureDate := time.Now().Add(90 * time.Second).UTC().Format(time.RFC1123)

	tests := []struct {
		name      string
		header    string
		wantWait  time.Duration
		wantOK    bool
		wantRange bool
	}{
		{"absent header", "", 0, false, false},
		{"seconds", "120", 120 * time.Second, true, false},
		{"seconds with whitespace", "  120  ", 120 * time.Second, true, false},
		{"zero seconds", "0", 0, true, false},
		{"negative seconds", "-5", 0, false, false},
		{"HTTP date in the future", futureDate, 90 * time.Second, true, true},
		{"HTTP date in the past", "Fri, 31 Dec 1999 23:59:59 GMT", 0, true, false},
		{"RFC 850 date", "Friday, 31-Dec-99 23:59:59 GMT", 0, true, false},
		{"asctime date", "Fri Dec 31 23:59:59 1999", 0, true, false},
		{"unparseable", "soon", 0, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := &http.Response{Header: make(http.Header)}

			if tt.header != "" {
				res.Header.Set("Retry-After", tt.header)
			}

			wait, ok := retryAfterWait(res)

			assert.Equal(t, tt.wantOK, ok)

			if tt.wantRange {
				// The wall clock advanced between formatting the header and
				// parsing it, so the wait lands slightly below the nominal
				// value.
				assert.Greater(t, wait, 80*time.Second)
				assert.LessOrEqual(t, wait, tt.wantWait)
			} else {
				assert.Equal(t, tt.wantWait, wait)
			}
		})
	}
}

func TestRetryableError(t *testing.T) {
	t.Parallel()

	inner := errors.New("inner failure")
	marked := &retryableError{err: inner}

	assert.Equal(t, "inner failure", marked.Error())
	assert.Same(t, inner, marked.Unwrap())
	require.ErrorIs(t, marked, inner)
}

func TestIsPermanentTransportError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "connection reset",
			err:  errors.New("connection reset by peer"),
			want: false,
		},
		{
			name: "exhausted redirects",
			err: &url.Error{
				Op:  "Get",
				URL: "http://example.com",
				Err: errors.New("stopped after 10 redirects"),
			},
			want: true,
		},
		{
			name: "unsupported scheme",
			err: &url.Error{
				Op:  "Get",
				URL: "gopher://example.com",
				Err: errors.New(`unsupported protocol scheme "gopher"`),
			},
			want: true,
		},
		{
			name: "certificate",
			err: &url.Error{
				Op:  "Get",
				URL: "https://example.com",
				Err: x509.UnknownAuthorityError{},
			},
			want: true,
		},
		{
			name: "nxdomain",
			err: &url.Error{
				Op:  "Get",
				URL: "https://example.com",
				Err: &net.DNSError{Err: "no such host", Name: "example.com", IsNotFound: true},
			},
			want: true,
		},
		{
			name: "dns timeout",
			err: &url.Error{
				Op:  "Get",
				URL: "https://example.com",
				Err: &net.DNSError{Err: "i/o timeout", Name: "example.com", IsTimeout: true},
			},
			want: false,
		},
		{
			name: "context deadline",
			err:  context.DeadlineExceeded,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isPermanentTransportError(tt.err))
		})
	}
}

func TestCanReplayRequestBody(t *testing.T) {
	t.Parallel()

	assert.True(t, canReplayRequestBody(&http.Request{}))
	assert.True(t, canReplayRequestBody(&http.Request{Body: http.NoBody}))

	reusable, err := hqgohttprequest.NewReusableReadCloser("payload")

	require.NoError(t, err)
	assert.True(t, canReplayRequestBody(&http.Request{Body: reusable}))

	assert.False(t, canReplayRequestBody(&http.Request{Body: io.NopCloser(strings.NewReader("payload"))}))
	assert.True(t, canReplayRequestBody(&http.Request{
		Body: io.NopCloser(strings.NewReader("payload")),
		GetBody: func() (io.ReadCloser, error) {
			return http.NoBody, nil
		},
	}))
}

func TestCanResendRequest(t *testing.T) {
	t.Parallel()

	assert.False(t, canResendRequest(&http.Request{Method: http.MethodPost}, true))
	assert.True(t, canResendRequest(&http.Request{Method: http.MethodPost}, false))
	assert.True(t, canResendRequest(&http.Request{Method: http.MethodGet}, true))
	assert.True(t, canResendRequest(&http.Request{Method: "QUERY"}, true))
	assert.False(t, canResendRequest(&http.Request{
		Method: http.MethodGet,
		Body:   io.NopCloser(strings.NewReader("payload")),
	}, true))
}

func TestIsIdempotentMethod(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		method     string
		idempotent bool
	}{
		{http.MethodGet, true},
		{http.MethodHead, true},
		{http.MethodOptions, true},
		{http.MethodTrace, true},
		{http.MethodPut, true},
		{http.MethodDelete, true},
		{http.MethodPost, false},
		{http.MethodPatch, false},
		{"get", true},
		{"POST", false},
	}

	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.idempotent, isIdempotentMethod(tc.method))
		})
	}
}

func TestRewindRequestBody(t *testing.T) {
	t.Parallel()

	t.Run("nil body", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, rewindRequestBody(&http.Request{}))
	})

	t.Run("no body", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, rewindRequestBody(&http.Request{Body: http.NoBody}))
	})

	t.Run("reusable body is rewound", func(t *testing.T) {
		t.Parallel()

		reusable, err := hqgohttprequest.NewReusableReadCloser("payload")

		require.NoError(t, err)

		buf := make([]byte, 2)

		_, err = reusable.Read(buf)

		require.NoError(t, err)

		require.NoError(t, rewindRequestBody(&http.Request{Body: reusable}))

		buf = make([]byte, len("payload"))

		n, err := reusable.Read(buf)

		require.NoError(t, err)
		assert.Equal(t, "payload", string(buf[:n]))
	})

	t.Run("body without get body is not replayable", func(t *testing.T) {
		t.Parallel()

		req := &http.Request{Body: io.NopCloser(strings.NewReader("original"))}

		require.ErrorIs(t, rewindRequestBody(req), ErrBodyNotReplayable)
	})

	t.Run("get body failure", func(t *testing.T) {
		t.Parallel()

		getBodyErr := errors.New("get body failed")

		req := &http.Request{
			Body: io.NopCloser(strings.NewReader("original")),
			GetBody: func() (body io.ReadCloser, err error) {
				return nil, getBodyErr
			},
		}

		require.ErrorIs(t, rewindRequestBody(req), ErrBodyNotReplayable)
		require.ErrorIs(t, rewindRequestBody(req), getBodyErr)
	})

	t.Run("get body success", func(t *testing.T) {
		t.Parallel()

		req := &http.Request{
			Body: io.NopCloser(strings.NewReader("original")),
			GetBody: func() (body io.ReadCloser, err error) {
				return io.NopCloser(strings.NewReader("refetched")), nil
			},
		}

		require.NoError(t, rewindRequestBody(req))

		body, err := io.ReadAll(req.Body)

		require.NoError(t, err)
		assert.Equal(t, "refetched", string(body))
	})
}

func TestIsHTTP1BrokenTransportError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "malformed HTTP version",
			err:  errors.New(`net/http: HTTP/1.x transport connection broken: malformed HTTP version "HTTP/2"`),
			want: true,
		},
		{
			name: "malformed HTTP response",
			err:  errors.New(`net/http: HTTP/1.x transport connection broken: malformed HTTP response "\x00\x00"`),
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("connection reset by peer"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, isHTTP1BrokenTransportError(tc.err))
		})
	}
}

func TestRetryError_FinalOutcome(t *testing.T) {
	t.Parallel()

	err := &RetryError{
		Attempts:   1,
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Err:        ErrBodyNotReplayable,
		final:      true,
	}

	require.NotErrorIs(t, err, ErrRetriesExhausted)
	require.ErrorIs(t, err, ErrBodyNotReplayable)
	assert.Contains(t, err.Error(), ErrBodyNotReplayable.Error())
}

func TestRetryError_NilReceiver(t *testing.T) {
	t.Parallel()

	var err *RetryError

	assert.True(t, err.Is(ErrRetriesExhausted))
	assert.False(t, err.Is(ErrBodyNotReplayable))
	assert.Equal(t, "http.Client.Do: giving up", err.Error())
}

func TestRetryAfterWaitOverflow(t *testing.T) {
	t.Parallel()

	res := &http.Response{Header: http.Header{"Retry-After": {"9223372037"}}}

	wait, ok := retryAfterWait(res)

	require.True(t, ok)
	assert.Positive(t, wait)
}
