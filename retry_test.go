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

	client := hqgohttp.New(hqgohttp.WithRetryMax(1),
		hqgohttp.WithRetryBackoff(noWaitBackoff),
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
		hqgohttp.WithRetryMax(2),
		hqgohttp.WithRetryBackoff(noWaitBackoff),
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

	client := hqgohttp.New(hqgohttp.WithRetryMax(1),
		hqgohttp.WithRetryBackoff(noWaitBackoff),
	)

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

	client := hqgohttp.New(
		hqgohttp.WithRetryMax(0),
	)

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

	client := hqgohttp.New(hqgohttp.WithRetryMax(1),
		hqgohttp.WithRetryBackoff(noWaitBackoff),
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

	client := hqgohttp.New(hqgohttp.WithRetryMax(1),
		hqgohttp.WithRetryBackoff(noWaitBackoff),
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

	client := hqgohttp.New(
		hqgohttp.WithRetryMax(0),
		hqgohttp.WithRetryBackoff(noWaitBackoff),
	)

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
