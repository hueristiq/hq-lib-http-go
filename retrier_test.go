package http_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestClientRetryAfterWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		retryAfter   string
		wantWait     time.Duration
		wantPositive bool
	}{
		{name: "seconds capped", retryAfter: "10", wantWait: 2 * time.Second},
		{name: "zero", retryAfter: "0", wantWait: time.Millisecond},
		{name: "absent", wantPositive: true},
		{name: "invalid", retryAfter: "later", wantPositive: true},
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

			var got time.Duration

			client := mustNew(t,
				hqgohttp.WithRetryMaxAttempts(2),
				hqgohttp.WithOnRetry(func(event hqgohttp.RetryEvent) {
					got = event.Wait
				}),
			)

			res, err := client.Do(newRequest(t, http.MethodGet, server.URL, http.NoBody))
			require.NoError(t, err)
			require.NoError(t, res.Body.Close())

			if tt.wantPositive {
				assert.Positive(t, got)
			} else {
				assert.Equal(t, tt.wantWait, got)
			}

			assert.LessOrEqual(t, got, 2*time.Second)
		})
	}
}

func TestDefaultRetryPolicyResponseWithErrorIsFinal(t *testing.T) {
	t.Parallel()

	policyErr := errors.New("redirect refused")
	resp := &http.Response{StatusCode: http.StatusInternalServerError}

	retry, rerr := hqgohttp.DefaultRetryPolicy(t.Context(), nil, resp, policyErr, 1)
	assert.False(t, retry)
	require.NoError(t, rerr)

	retry, rerr = hqgohttp.DefaultRetryPolicy(t.Context(), nil, nil, policyErr, 1)
	assert.True(t, retry)
	require.NoError(t, rerr)
}

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()

	errConnection := errors.New("connection refused")
	tests := []struct {
		name      string
		resp      *http.Response
		err       error
		wantRetry bool
		wantErr   bool
	}{
		{name: "connection error is retried", err: &url.Error{Op: "Get", URL: "http://example.com", Err: errConnection}, wantRetry: true},
		{name: "bare transport error is retried", err: errConnection, wantRetry: true},
		{name: "exhausted redirect limit is not retried", err: &url.Error{Op: "Get", URL: "http://example.com", Err: errors.New("stopped after 10 redirects")}},
		{name: "unsupported scheme is not retried", err: &url.Error{Op: "Get", URL: "gopher://example.com", Err: errors.New(`unsupported protocol scheme "gopher"`)}},
		{name: "unknown authority is not retried", err: &url.Error{Op: "Get", URL: "https://example.com", Err: x509.UnknownAuthorityError{}}},
		{name: "certificate verification failure is not retried", err: &url.Error{Op: "Get", URL: "https://example.com", Err: &tls.CertificateVerificationError{}}},
		{name: "429 is retried", resp: &http.Response{StatusCode: http.StatusTooManyRequests}, wantRetry: true},
		{name: "408 is retried", resp: &http.Response{StatusCode: http.StatusRequestTimeout}, wantRetry: true},
		{name: "nxdomain is not retried", err: &url.Error{Op: "Get", URL: "https://example.com", Err: &net.DNSError{Err: "no such host", Name: "example.com", IsNotFound: true}}},
		{name: "dns timeout is retried", err: &url.Error{Op: "Get", URL: "https://example.com", Err: &net.DNSError{Err: "i/o timeout", Name: "example.com", IsTimeout: true}}, wantRetry: true},
		{name: "500 is retried", resp: &http.Response{StatusCode: http.StatusInternalServerError}, wantRetry: true},
		{name: "503 is retried", resp: &http.Response{StatusCode: http.StatusServiceUnavailable}, wantRetry: true},
		{name: "501 is not retried", resp: &http.Response{StatusCode: http.StatusNotImplemented}},
		{name: "zero status is retried", resp: &http.Response{StatusCode: 0}, wantRetry: true},
		{name: "200 is not retried", resp: &http.Response{StatusCode: http.StatusOK}},
		{name: "404 is not retried", resp: &http.Response{StatusCode: http.StatusNotFound}},
		{name: "nil response and nil error is not retried"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			retry, rerr := hqgohttp.DefaultRetryPolicy(t.Context(), nil, tt.resp, tt.err, 1)
			assert.Equal(t, tt.wantRetry, retry)

			if tt.wantErr {
				require.Error(t, rerr)
			} else {
				assert.NoError(t, rerr)
			}
		})
	}
}

func TestDefaultRetryPolicyReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	retry, rerr := hqgohttp.DefaultRetryPolicy(ctx, nil, nil, context.Canceled, 1)
	assert.False(t, retry)
	require.ErrorIs(t, rerr, context.Canceled)
}
