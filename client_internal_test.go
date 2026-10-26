package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
)

// roundTripFunc adapts a function to an [http.RoundTripper] for scripted
// transports.
type roundTripFunc func(req *http.Request) (res *http.Response, err error)

// RoundTrip executes the scripted function.
//
// Parameters:
//   - req (*http.Request): the request being round-tripped.
//
// Returns:
//   - res (*http.Response): the scripted response.
//   - err (error): the scripted error.
func (f roundTripFunc) RoundTrip(req *http.Request) (res *http.Response, err error) {
	res, err = f(req)

	return
}

// newOKResponse builds a minimal 200 OK response with the given body.
//
// Parameters:
//   - body (string): the response body.
//
// Returns:
//   - res (*http.Response): the response.
func newOKResponse(body string) (res *http.Response) {
	res = &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}

	return
}

// errHTTP1Broken is the failure net/http produces when a cleartext HTTP/1.x
// exchange receives HTTP/2 frames. It is message-only in the standard
// library, so tests reproduce the message.
var errHTTP1Broken = errors.New(`net/http: HTTP/1.x transport connection broken: malformed HTTP version "HTTP/2"`)

func TestClient_CloseIdleConnectionsIfDue(t *testing.T) {
	t.Parallel()

	t.Run("disabled by default", func(t *testing.T) {
		t.Parallel()

		var closes atomic.Int32

		transport := &closeCountingTransport{closes: &closes}

		client := New(WithHTTPClient(&http.Client{Transport: transport}))

		for range 10 {
			client.closeIdleConnectionsIfDue()
		}

		assert.Zero(t, closes.Load())
	})

	t.Run("closes at the threshold", func(t *testing.T) {
		t.Parallel()

		var closes atomic.Int32

		transport := &closeCountingTransport{closes: &closes}

		client := New(
			WithHTTPClient(&http.Client{Transport: transport}),
			WithMaxRequestsBeforeIdleClose(3),
		)

		for range 7 {
			client.closeIdleConnectionsIfDue()
		}

		assert.Equal(t, int32(2), closes.Load(),
			"Expected a close on the 3rd and 6th completed executions")
	})
}

// closeCountingTransport is an [http.RoundTripper] counting
// CloseIdleConnections calls.
//
// Fields:
//   - closes (*atomic.Int32): the counter incremented per close.
type closeCountingTransport struct {
	closes *atomic.Int32
}

// Compile-time guard ensuring [closeCountingTransport] satisfies
// [http.RoundTripper].
var _ http.RoundTripper = (*closeCountingTransport)(nil)

// RoundTrip answers every request with a 200 OK.
//
// Parameters:
//   - _ (*http.Request): the request being round-tripped.
//
// Returns:
//   - res (*http.Response): a 200 OK response.
//   - err (error): always nil.
func (t *closeCountingTransport) RoundTrip(_ *http.Request) (res *http.Response, err error) {
	res = newOKResponse("OK")

	return
}

// CloseIdleConnections increments the close counter.
func (t *closeCountingTransport) CloseIdleConnections() {
	t.closes.Add(1)
}

// countingReadCloser wraps a reader, counting the bytes read and recording
// whether Close was called.
//
// Fields:
//   - Reader (io.Reader): the underlying reader.
//   - read (atomic.Int64): the number of bytes read.
//   - closed (atomic.Bool): whether Close was called.
type countingReadCloser struct {
	io.Reader
	read   atomic.Int64
	closed atomic.Bool
}

// Read reads from the underlying reader and counts the bytes.
//
// Parameters:
//   - p ([]byte): the buffer to read into.
//
// Returns:
//   - n (int): the number of bytes read.
//   - err (error): non-nil when the read fails.
func (c *countingReadCloser) Read(p []byte) (n int, err error) {
	n, err = c.Reader.Read(p)

	c.read.Add(int64(n))

	return
}

// Close records the close.
//
// Returns:
//   - err (error): always nil.
func (c *countingReadCloser) Close() (err error) {
	c.closed.Store(true)

	return
}

func TestClient_RetriedResponseBodyIsDrainedWithinLimit(t *testing.T) {
	t.Parallel()

	// newRetryingClient starts a client whose scripted transport answers 503
	// with a large body on the first attempt and 200 on the second, and stores
	// the first attempt's body into firstBody for inspection.
	newRetryingClient := func(t *testing.T, firstBody **countingReadCloser) (client *Client) {
		t.Helper()

		var attempts atomic.Int32

		transport := roundTripFunc(func(_ *http.Request) (res *http.Response, err error) {
			if attempts.Add(1) == 1 {
				*firstBody = &countingReadCloser{Reader: strings.NewReader(strings.Repeat("x", 1<<20))}

				res = &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Status:     "503 Service Unavailable",
					Header:     make(http.Header),
					Body:       *firstBody,
				}

				return
			}

			res = newOKResponse("recovered")

			return
		})

		client = New(
			WithHTTPClient(&http.Client{Transport: transport}),
			WithRetryWaitMin(time.Millisecond),
			WithRetryWaitMax(time.Millisecond),
		)

		return
	}

	t.Run("client response read limit", func(t *testing.T) {
		t.Parallel()

		var firstBody *countingReadCloser

		client := newRetryingClient(t, &firstBody)

		req, err := hqgohttprequest.New(t.Context(), "GET", "http://example.com/", nil)

		require.NoError(t, err)

		res, err := client.Do(req)

		require.NoError(t, err)
		require.NotNil(t, res)

		body, err := io.ReadAll(res.Body)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, "recovered", string(body))
		require.NotNil(t, firstBody)
		assert.True(t, firstBody.closed.Load(), "Expected the retried response body to be closed")
		assert.Positive(t, firstBody.read.Load(), "Expected the retried response body to be drained")
		assert.LessOrEqual(t, firstBody.read.Load(), int64(DefaultResponseReadLimit)+1,
			"Expected the drain to be bounded by the response read limit")
	})

	t.Run("request response read limit override", func(t *testing.T) {
		t.Parallel()

		const limit int64 = 16

		var firstBody *countingReadCloser

		client := newRetryingClient(t, &firstBody)

		req, err := hqgohttprequest.New(t.Context(), "GET", "http://example.com/", nil)

		require.NoError(t, err)

		res, err := client.Do(req, WithRequestResponseReadLimit(limit))

		require.NoError(t, err)
		require.NotNil(t, res)

		body, err := io.ReadAll(res.Body)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, "recovered", string(body))
		require.NotNil(t, firstBody)
		assert.True(t, firstBody.closed.Load(), "Expected the retried response body to be closed")
		assert.Positive(t, firstBody.read.Load(), "Expected the retried response body to be drained")
		assert.LessOrEqual(t, firstBody.read.Load(), limit+1,
			"Expected the drain to be bounded by the per-request override")
	})
}

func TestClient_Do_TransportReturnsNoOutcome(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(_ *http.Request) (res *http.Response, err error) {
		return nil, nil //nolint:nilnil // Exercises the transport contract violation guard.
	})

	client := New(
		WithHTTPClient(&http.Client{Transport: transport}),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), "GET", "http://example.com/", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.Error(t, err)
	assert.Nil(t, res)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	assert.Contains(t, err.Error(), "returned a nil *Response with a nil error")
}

// testRateLimiter is a [RateLimiter] whose waits always succeed.
type testRateLimiter struct{}

// Wait reports success immediately.
//
// Parameters: none.
//
// Returns:
//   - err (error): always nil.
func (testRateLimiter) Wait(_ context.Context) (err error) {
	return nil
}

// reviewErrorBody is an [io.ReadCloser] whose reads fail with a fixed error.
type reviewErrorBody struct {
	err error
}

// Read returns the configured error.
func (b reviewErrorBody) Read([]byte) (n int, err error) {
	return 0, b.err
}

// Close succeeds.
func (b reviewErrorBody) Close() error {
	return nil
}

func TestWithTLSConfig_Clones(t *testing.T) {
	t.Parallel()

	protocols := []Protocol{
		ProtocolAuto,
		ProtocolAutoHTTP3,
		ProtocolHTTP1,
		ProtocolHTTP2,
		ProtocolHTTP3,
	}

	for _, protocol := range protocols {
		t.Run(protocol.String(), func(t *testing.T) {
			t.Parallel()

			config := &tls.Config{ServerName: "original.example"}

			client := New(WithProtocol(protocol), WithTLSConfig(config))

			config.ServerName = "mutated.example"

			switch transport := client.HTTPClient.Transport.(type) {
			case *http.Transport:
				require.NotNil(t, transport.TLSClientConfig)
				assert.Equal(t, "original.example", transport.TLSClientConfig.ServerName)
			case *http3.Transport:
				require.NotNil(t, transport.TLSClientConfig)
				assert.Equal(t, "original.example", transport.TLSClientConfig.ServerName)
			default:
				t.Fatalf("unexpected transport type %T", transport)
			}

			if client.fallbackHTTPClient != nil {
				transport, ok := client.fallbackHTTPClient.Transport.(*http.Transport)

				require.True(t, ok)
				require.NotNil(t, transport.TLSClientConfig)
				assert.Equal(t, "original.example", transport.TLSClientConfig.ServerName)
			}
		})
	}
}

func TestClientOptions_WithHeader(t *testing.T) {
	t.Parallel()

	opts := &ClientOptions{}

	WithHeader("", "ignored")(opts)

	assert.Empty(t, opts.Headers, "Expected an empty key to be ignored")

	WithHeader("X-Test", "first")(opts)
	WithHeader("x-test", "second")(opts)

	require.Len(t, opts.Headers, 1, "Expected a case-insensitive key to replace the existing header")
	assert.Equal(t, "X-Test", opts.Headers[0].Key)
	assert.Equal(t, "second", opts.Headers[0].Value)

	WithHeader("X-Other", "third")(opts)

	require.Len(t, opts.Headers, 2)
	assert.Equal(t, "X-Other", opts.Headers[1].Key)
	assert.Equal(t, "third", opts.Headers[1].Value)
}

func TestClient_ExecutionTimeoutDefaultsToDisabled(t *testing.T) {
	t.Parallel()

	assert.Zero(t, New().Timeout)
	assert.Zero(t, New(WithTimeout(0)).Timeout)
	assert.Zero(t, New(WithTimeout(-time.Second)).Timeout)
	assert.Equal(t, RecommendedTimeout, New(WithTimeout(RecommendedTimeout)).Timeout)
}

func TestWithAttemptTimeout(t *testing.T) {
	t.Parallel()

	opts := &ClientOptions{AttemptTimeout: time.Second}

	WithAttemptTimeout(0)(opts)

	assert.Zero(t, opts.AttemptTimeout)

	WithAttemptTimeout(2 * time.Second)(opts)

	assert.Equal(t, 2*time.Second, opts.AttemptTimeout)
}

func TestSameRoundTripper(t *testing.T) {
	t.Parallel()

	assert.True(t, sameRoundTripper(nil, nil))
	assert.False(t, sameRoundTripper(nil, &http.Transport{}))

	transport := &http.Transport{}

	assert.True(t, sameRoundTripper(transport, transport))
	assert.False(t, sameRoundTripper(transport, &http.Transport{}))

	first := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, nil //nolint:nilnil // Only the function value is compared.
	})
	second := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, nil //nolint:nilnil // Only the function value is compared.
	})

	assert.False(t, sameRoundTripper(first, second), "Expected function transports to be uncomparable")
}

func TestClientOptions_NormalizeResponseBodyTimeout(t *testing.T) {
	t.Parallel()

	opts := &ClientOptions{ResponseBodyTimeout: -time.Second}

	opts.normalize()

	assert.Zero(t, opts.ResponseBodyTimeout)
}

func TestRequestOptions_Normalize(t *testing.T) {
	t.Parallel()

	opts := &RequestOptions{
		MaxRetries:        -1,
		RetryWaitMin:      0,
		RetryWaitMax:      0,
		ResponseReadLimit: 0,
	}

	opts.normalize()

	assert.Equal(t, DefaultRetryMax, opts.MaxRetries)
	assert.Equal(t, DefaultRetryWaitMin, opts.RetryWaitMin)
	assert.Equal(t, DefaultRetryWaitMax, opts.RetryWaitMax)
	assert.NotNil(t, opts.NewRetryBackoff)
	assert.NotNil(t, opts.RetryPolicy)
	assert.Equal(t, int64(DefaultResponseReadLimit), opts.ResponseReadLimit)

	opts = &RequestOptions{
		MaxRetries:        5,
		RetryWaitMin:      time.Second,
		RetryWaitMax:      500 * time.Millisecond,
		ResponseReadLimit: 128,
	}

	opts.normalize()

	assert.Equal(t, 5, opts.MaxRetries)
	assert.Equal(t, time.Second, opts.RetryWaitMax, "Expected the maximum wait to be raised to the minimum")
	assert.Equal(t, int64(128), opts.ResponseReadLimit)
}

func TestProtocol_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		protocol Protocol
		expected string
	}{
		{ProtocolAuto, "auto"},
		{ProtocolAutoHTTP3, "auto-http3"},
		{ProtocolHTTP1, "http/1.1"},
		{ProtocolHTTP2, "http/2"},
		{ProtocolHTTP3, "http/3"},
		{Protocol(99), "auto"},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, test.protocol.String())
	}
}

func TestNew_ProtocolSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		options  []ClientOptionFunc
		validate func(t *testing.T, client *Client)
	}{
		{
			name:    "auto",
			options: []ClientOptionFunc{WithProtocol(ProtocolAuto)},
			validate: func(t *testing.T, client *Client) {
				t.Helper()

				transport, ok := client.HTTPClient.Transport.(*http.Transport)

				require.True(t, ok)
				assert.Nil(t, transport.Protocols)
				assert.True(t, transport.ForceAttemptHTTP2)
				assert.NotNil(t, client.fallbackHTTPClient)
				assert.False(t, client.fallbackOnHTTP3)
			},
		},
		{
			name:    "auto http3",
			options: []ClientOptionFunc{WithProtocol(ProtocolAutoHTTP3)},
			validate: func(t *testing.T, client *Client) {
				t.Helper()

				_, ok := client.HTTPClient.Transport.(*http3.Transport)

				assert.True(t, ok)
				assert.NotNil(t, client.fallbackHTTPClient)
				assert.True(t, client.fallbackOnHTTP3)
			},
		},
		{
			name:    "http1",
			options: []ClientOptionFunc{WithProtocol(ProtocolHTTP1)},
			validate: func(t *testing.T, client *Client) {
				t.Helper()

				transport, ok := client.HTTPClient.Transport.(*http.Transport)

				require.True(t, ok)
				require.NotNil(t, transport.Protocols)
				assert.True(t, transport.Protocols.HTTP1())
				assert.False(t, transport.Protocols.HTTP2())
				assert.False(t, transport.Protocols.UnencryptedHTTP2())
				assert.False(t, transport.ForceAttemptHTTP2)
				assert.Nil(t, client.fallbackHTTPClient)
			},
		},
		{
			name:    "http2",
			options: []ClientOptionFunc{WithProtocol(ProtocolHTTP2)},
			validate: func(t *testing.T, client *Client) {
				t.Helper()

				transport, ok := client.HTTPClient.Transport.(*http.Transport)

				require.True(t, ok)
				require.NotNil(t, transport.Protocols)
				assert.False(t, transport.Protocols.HTTP1())
				assert.True(t, transport.Protocols.HTTP2())
				assert.True(t, transport.Protocols.UnencryptedHTTP2())
				assert.Nil(t, client.fallbackHTTPClient)
			},
		},
		{
			name:    "http3",
			options: []ClientOptionFunc{WithProtocol(ProtocolHTTP3)},
			validate: func(t *testing.T, client *Client) {
				t.Helper()

				_, ok := client.HTTPClient.Transport.(*http3.Transport)

				assert.True(t, ok)
				assert.Nil(t, client.fallbackHTTPClient)
				assert.False(t, client.fallbackOnHTTP3)
			},
		},
		{
			name: "custom client wins",
			options: []ClientOptionFunc{
				WithHTTPClient(&http.Client{
					Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
						return newOKResponse("custom"), nil
					}),
				}),
				WithProtocol(ProtocolHTTP3),
			},
			validate: func(t *testing.T, client *Client) {
				t.Helper()

				transport, ok := client.HTTPClient.Transport.(roundTripFunc)

				require.True(t, ok)
				assert.NotNil(t, client.fallbackHTTPClient)
				assert.False(t, client.fallbackOnHTTP3)

				res, err := transport.RoundTrip(nil)

				require.NoError(t, err)
				assert.Equal(t, "custom", readRawBody(t, res))
			},
		},
		{
			name:    "invalid protocol falls back to auto",
			options: []ClientOptionFunc{WithProtocol(Protocol(99))},
			validate: func(t *testing.T, client *Client) {
				t.Helper()

				transport, ok := client.HTTPClient.Transport.(*http.Transport)

				require.True(t, ok)
				assert.Nil(t, transport.Protocols)
				assert.NotNil(t, client.fallbackHTTPClient)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := New(test.options...)

			require.NotNil(t, client)

			test.validate(t, client)
		})
	}
}

// readBody reads and closes a wrapped response body.
func readBody(t *testing.T, res *Response) (body string) {
	t.Helper()

	require.NotNil(t, res)

	body = readRawBody(t, res.Response)

	return body
}

// readRawBody reads and closes a raw response body.
func readRawBody(t *testing.T, res *http.Response) (body string) {
	t.Helper()

	require.NotNil(t, res)

	content, err := io.ReadAll(res.Body)

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	body = string(content)

	return body
}

func TestIsHTTP3FallbackError(t *testing.T) {
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
			name: "canceled",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "deadline",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "certificate",
			err:  &url.Error{Op: "Get", URL: "https://example.com", Err: x509.UnknownAuthorityError{}},
			want: false,
		},
		{
			name: "redirect limit",
			err:  &url.Error{Op: "Get", URL: "http://example.com", Err: errors.New("stopped after 10 redirects")},
			want: false,
		},
		{
			name: "unsupported scheme",
			err:  &url.Error{Op: "Get", URL: "gopher://example.com", Err: errors.New(`unsupported protocol scheme "gopher"`)},
			want: false,
		},
		{
			name: "nxdomain",
			err:  &url.Error{Op: "Get", URL: "https://example.com", Err: &net.DNSError{Err: "no such host", IsNotFound: true}},
			want: false,
		},
		{
			name: "connection reset",
			err:  errors.New("connection reset by peer"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isHTTP3FallbackError(tt.err))
		})
	}
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_Do_AutoHTTP3RedirectAbortIsFinal(t *testing.T) {
	var h3Calls, autoCalls atomic.Int32

	swapHTTP3Seams(t,
		func(*tls.Config) *http.Client {
			return &http.Client{
				Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
					h3Calls.Add(1)

					res := newOKResponse("redirect")
					res.StatusCode = http.StatusFound
					res.Status = "302 Found"
					res.Header.Set("Location", "https://elsewhere.example/")

					return res, nil
				}),
			}
		},
		countingClient(&autoCalls, "auto"),
	)

	redirectErr := errors.New("redirects are forbidden")

	client := New(
		WithProtocol(ProtocolAutoHTTP3),
		withoutProxy(),
		WithRetryMax(0),
		WithCheckRedirect(func(*http.Request, []*http.Request) error {
			return redirectErr
		}),
	)

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com/", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.Error(t, err)
	require.ErrorContains(t, err, "redirects are forbidden")
	assert.Equal(t, int32(1), h3Calls.Load())
	assert.Zero(t, autoCalls.Load(), "Expected a redirect-policy abort not to trigger the fallback")
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_Do_AutoHTTP3DoesNotFallBackOnPermanentError(t *testing.T) {
	var h3Calls, autoCalls atomic.Int32

	swapHTTP3Seams(t,
		func(*tls.Config) *http.Client {
			return &http.Client{
				Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
					h3Calls.Add(1)

					return nil, &url.Error{
						Op:  "Get",
						URL: "https://example.com/",
						Err: x509.UnknownAuthorityError{},
					}
				}),
			}
		},
		countingClient(&autoCalls, "auto"),
	)

	client := New(
		WithProtocol(ProtocolAutoHTTP3),
		withoutProxy(),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com/", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.Error(t, err)
	assert.Nil(t, res)
	assert.Equal(t, int32(1), h3Calls.Load())
	assert.Zero(t, autoCalls.Load(), "Expected a certificate failure not to trigger the fallback")
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_HTTP3FailureCache(t *testing.T) {
	t.Run("a failed host skips HTTP/3 for the TTL", func(t *testing.T) {
		var h3Calls, autoCalls atomic.Int32

		swapHTTP3Seams(t,
			func(*tls.Config) *http.Client {
				return &http.Client{
					Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
						h3Calls.Add(1)

						return nil, errors.New("h3 unavailable")
					}),
				}
			},
			countingClient(&autoCalls, "auto"),
		)

		client := New(
			WithProtocol(ProtocolAutoHTTP3),
			withoutProxy(),
			WithRetryMax(0),
		)

		for range 2 {
			req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com/", nil)

			require.NoError(t, err)

			res, err := client.Do(req)

			require.NoError(t, err)
			require.NotNil(t, res)
			assert.Equal(t, "auto", readBody(t, res))
			require.NoError(t, res.Body.Close())
		}

		assert.Equal(t, int32(1), h3Calls.Load())
		assert.Equal(t, int32(2), autoCalls.Load())
	})

	t.Run("a disabled TTL retries HTTP/3 every request", func(t *testing.T) {
		var h3Calls, autoCalls atomic.Int32

		swapHTTP3Seams(t,
			func(*tls.Config) *http.Client {
				return &http.Client{
					Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
						h3Calls.Add(1)

						return nil, errors.New("h3 unavailable")
					}),
				}
			},
			countingClient(&autoCalls, "auto"),
		)

		client := New(
			WithProtocol(ProtocolAutoHTTP3),
			WithHTTP3FallbackTTL(0),
			withoutProxy(),
			WithRetryMax(0),
		)

		for range 2 {
			req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com/", nil)

			require.NoError(t, err)

			res, err := client.Do(req)

			require.NoError(t, err)
			require.NotNil(t, res)
			require.NoError(t, res.Body.Close())
		}

		assert.Equal(t, int32(2), h3Calls.Load())
		assert.Equal(t, int32(2), autoCalls.Load())
	})

	t.Run("an expired entry is discarded", func(t *testing.T) {
		client := New(
			WithProtocol(ProtocolAutoHTTP3),
			WithHTTP3FallbackTTL(time.Minute),
		)

		client.recordHTTP3Failure("example.com")

		assert.True(t, client.http3RecentlyFailed("example.com"))

		client.http3Failures.Store("example.com", time.Now().Add(-2*time.Minute))

		assert.False(t, client.http3RecentlyFailed("example.com"))

		_, ok := client.http3Failures.Load("example.com")
		assert.False(t, ok, "Expected the expired entry to be deleted")
	})

	t.Run("a success clears an entry", func(t *testing.T) {
		client := New(
			WithProtocol(ProtocolAutoHTTP3),
			WithHTTP3FallbackTTL(time.Minute),
		)

		client.recordHTTP3Failure("example.com")

		assert.True(t, client.http3RecentlyFailed("example.com"))

		client.clearHTTP3Failure("example.com")

		assert.False(t, client.http3RecentlyFailed("example.com"))
	})
}

func TestClient_WithHTTP3TransportBuilder(t *testing.T) {
	t.Parallel()

	t.Run("applies to the built HTTP/3 transport", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		client := New(
			WithProtocol(ProtocolHTTP3),
			WithHTTP3TransportBuilder(func(transport *http3.Transport) *http3.Transport {
				calls.Add(1)

				transport.MaxResponseHeaderBytes = 1234

				return transport
			}),
		)

		assert.Equal(t, int32(1), calls.Load())

		transport, ok := client.HTTPClient.Transport.(*http3.Transport)

		require.True(t, ok)
		assert.Equal(t, 1234, transport.MaxResponseHeaderBytes)
	})

	t.Run("ignored with a custom transport", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		client := New(
			WithProtocol(ProtocolHTTP3),
			WithTransport(&http.Transport{}),
			WithHTTP3TransportBuilder(func(transport *http3.Transport) *http3.Transport {
				calls.Add(1)

				return transport
			}),
		)

		assert.Zero(t, calls.Load())
		assert.IsType(t, &http.Transport{}, client.HTTPClient.Transport)
	})

	t.Run("ignored with a custom client", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		client := New(
			WithHTTPClient(&http.Client{}),
			WithHTTP3TransportBuilder(func(transport *http3.Transport) *http3.Transport {
				calls.Add(1)

				return transport
			}),
		)

		require.NotNil(t, client)
		assert.Zero(t, calls.Load())
	})

	t.Run("nil result keeps the built transport", func(t *testing.T) {
		t.Parallel()

		client := New(
			WithProtocol(ProtocolHTTP3),
			WithHTTP3TransportBuilder(func(*http3.Transport) *http3.Transport {
				return nil
			}),
		)

		_, ok := client.HTTPClient.Transport.(*http3.Transport)

		assert.True(t, ok)
	})
}

func TestClient_HTTP3FailureCache_EmptyHost(t *testing.T) {
	t.Parallel()

	client := New(WithProtocol(ProtocolAutoHTTP3))

	client.recordHTTP3Failure("")
	assert.False(t, client.http3RecentlyFailed(""))

	client.clearHTTP3Failure("")
}

//nolint:paralleltest // The test substitutes package-level client seams, which are process-wide state.
func TestClient_Do_AutoHTTP3FallsBack(t *testing.T) {
	var h3Calls, fallbackCalls atomic.Int32

	originalHTTP3Client := newHTTP3Client
	originalAutoClient := newAutoHTTPClient

	t.Cleanup(func() {
		newHTTP3Client = originalHTTP3Client
		newAutoHTTPClient = originalAutoClient
	})

	newHTTP3Client = func(_ *tls.Config) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				h3Calls.Add(1)

				return nil, errors.New("h3 unavailable")
			}),
		}
	}

	newAutoHTTPClient = func(_ *tls.Config) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				fallbackCalls.Add(1)

				return newOKResponse("fallback body"), nil
			}),
		}
	}

	client := New(WithProtocol(ProtocolAutoHTTP3), withoutProxy(), WithRetryMax(0))

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "fallback body", readBody(t, res))
	assert.Equal(t, int32(1), h3Calls.Load())
	assert.Equal(t, int32(1), fallbackCalls.Load())
}

//nolint:paralleltest // The test substitutes package-level client seams, which are process-wide state.
func TestClient_Do_AutoHTTP3RewindsBody(t *testing.T) {
	var fallbackBody string

	originalHTTP3Client := newHTTP3Client
	originalAutoClient := newAutoHTTPClient

	t.Cleanup(func() {
		newHTTP3Client = originalHTTP3Client
		newAutoHTTPClient = originalAutoClient
	})

	newHTTP3Client = func(_ *tls.Config) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				_, _ = io.ReadAll(io.LimitReader(req.Body, req.ContentLength))

				return nil, errors.New("h3 unavailable")
			}),
		}
	}

	newAutoHTTPClient = func(_ *tls.Config) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, rerr := io.ReadAll(io.LimitReader(req.Body, req.ContentLength))
				if rerr != nil {
					return nil, rerr
				}

				fallbackBody = string(body)

				return newOKResponse("fallback body"), nil
			}),
		}
	}

	client := New(WithProtocol(ProtocolAutoHTTP3), withoutProxy(), WithRetryMax(0))

	req, err := hqgohttprequest.New(t.Context(), http.MethodPut, "https://example.com", []byte("payload"))

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "payload", fallbackBody)
	assert.Equal(t, "fallback body", readBody(t, res))
}

//nolint:paralleltest // The test substitutes package-level client seams, which are process-wide state.
func TestClient_Do_AutoHTTP3DoesNotFallBackOnContextError(t *testing.T) {
	var fallbackCalls atomic.Int32

	originalHTTP3Client := newHTTP3Client
	originalAutoClient := newAutoHTTPClient

	t.Cleanup(func() {
		newHTTP3Client = originalHTTP3Client
		newAutoHTTPClient = originalAutoClient
	})

	newHTTP3Client = func(_ *tls.Config) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return nil, context.Canceled
			}),
		}
	}

	newAutoHTTPClient = func(_ *tls.Config) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				fallbackCalls.Add(1)

				return newOKResponse("fallback body"), nil
			}),
		}
	}

	client := New(WithProtocol(ProtocolAutoHTTP3), withoutProxy(), WithRetryMax(0))

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, fallbackCalls.Load())
}

//nolint:paralleltest // substitutes a package-level seam, which is process-wide state.
func TestClient_Do_FallbackSkippedForNonIdempotent(t *testing.T) {
	var fallbackCalls atomic.Int32

	original := newFallbackClient

	t.Cleanup(func() {
		newFallbackClient = original
	})

	newFallbackClient = func(_ *http.Client, _ *tls.Config) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				fallbackCalls.Add(1)

				return newOKResponse("fallback body"), nil
			}),
		}
	}

	primary := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, errHTTP1Broken
	})

	t.Run("default idempotent-only", func(t *testing.T) {
		client := New(
			WithHTTPClient(&http.Client{Transport: primary}),
			WithRetryMax(0),
		)

		req, err := hqgohttprequest.New(t.Context(), http.MethodPost, "http://example.com/", nil)

		require.NoError(t, err)

		res, err := client.Do(req)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		require.ErrorIs(t, err, errHTTP1Broken)
		assert.Nil(t, res)
		assert.Zero(t, fallbackCalls.Load(),
			"Expected a POST not to be resent on the protocol fallback")
	})

	t.Run("opt out", func(t *testing.T) {
		client := New(
			WithHTTPClient(&http.Client{Transport: primary}),
			WithRetryIdempotent(false),
			WithRetryMax(0),
		)

		req, err := hqgohttprequest.New(t.Context(), http.MethodPost, "http://example.com/", nil)

		require.NoError(t, err)

		res, err := client.Do(req)

		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, "fallback body", readBody(t, res))
		assert.Equal(t, int32(1), fallbackCalls.Load())
	})
}

//nolint:paralleltest // The test substitutes the package-level newFallbackClient seam, which is process-wide state.
func TestClient_Do_FallbackSkippedForNonResendableBody(t *testing.T) {
	var fallbackCalls atomic.Int32

	original := newFallbackClient

	t.Cleanup(func() {
		newFallbackClient = original
	})

	newFallbackClient = func(_ *http.Client, _ *tls.Config) (client *http.Client) {
		client = &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (res *http.Response, err error) {
				fallbackCalls.Add(1)

				res = newOKResponse("fallback body")

				return
			}),
		}

		return
	}

	primary := roundTripFunc(func(req *http.Request) (res *http.Response, err error) {
		// A real transport consumes the body before reporting the failure.
		_, _ = io.Copy(io.Discard, req.Body)

		err = errHTTP1Broken

		return
	})

	// A policy that never retries keeps the attempt's original error visible.
	client := New(
		WithHTTPClient(&http.Client{Transport: primary}),
		WithRetryPolicy(func(_ context.Context, _ *http.Request, _ *http.Response, _ error, _ int) (retry bool, rerr error) {
			return
		}),
	)

	parsedURL, err := url.Parse("http://example.com/")

	require.NoError(t, err)

	// A hand-built request whose body is neither reusable nor refetchable.
	req := &hqgohttprequest.Request{Request: &http.Request{
		Method:        http.MethodPut,
		URL:           parsedURL,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader("not resendable")),
		ContentLength: int64(len("not resendable")),
	}}

	res, err := client.Do(req)

	require.ErrorIs(t, err, errHTTP1Broken)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	assert.Nil(t, res)
	assert.Zero(t, fallbackCalls.Load(),
		"Expected the fallback to be skipped when the body cannot be resent")
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_Do_AutoHTTP3SkipsH3WhenProxied(t *testing.T) {
	var h3Calls, autoCalls atomic.Int32

	swapHTTP3Seams(t, countingClient(&h3Calls, "h3"), countingClient(&autoCalls, "auto"))

	client := New(
		WithProtocol(ProtocolAutoHTTP3),
		fixedProxy(&url.URL{Scheme: "http", Host: "proxy.example"}),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "auto", readBody(t, res))
	assert.Zero(t, h3Calls.Load(), "Expected HTTP/3 to be skipped when a proxy applies")
	assert.Equal(t, int32(1), autoCalls.Load())
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_Do_HTTP3RejectsProxy(t *testing.T) {
	var h3Calls atomic.Int32

	swapHTTP3Seams(t, countingClient(&h3Calls, "h3"), nil)

	client := New(
		WithProtocol(ProtocolHTTP3),
		fixedProxy(&url.URL{Scheme: "http", Host: "proxy.example"}),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.ErrorIs(t, err, ErrHTTP3ProxyUnsupported)
	assert.Nil(t, res)
	assert.Zero(t, h3Calls.Load())
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_Do_AutoHTTP3UsesH3WhenSelectorReturnsNil(t *testing.T) {
	var h3Calls, autoCalls atomic.Int32

	swapHTTP3Seams(t, countingClient(&h3Calls, "h3"), countingClient(&autoCalls, "auto"))

	client := New(
		WithProtocol(ProtocolAutoHTTP3),
		WithProxy(func(*http.Request) (*url.URL, error) {
			return nil, nil //nolint:nilnil // A nil proxy means the request is sent directly.
		}),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "h3", readBody(t, res))
	assert.Equal(t, int32(1), h3Calls.Load())
	assert.Zero(t, autoCalls.Load())
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_Do_HTTP3UsesH3WhenProxyDisabled(t *testing.T) {
	var h3Calls atomic.Int32

	swapHTTP3Seams(t, countingClient(&h3Calls, "h3"), nil)

	client := New(
		WithProtocol(ProtocolHTTP3),
		withoutProxy(),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "h3", readBody(t, res))
	assert.Equal(t, int32(1), h3Calls.Load())
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_Do_HTTP3WithCustomTransportDoesNotGate(t *testing.T) {
	var h3Calls atomic.Int32

	swapHTTP3Seams(t, countingClient(&h3Calls, "h3"), nil)

	custom := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return newOKResponse("custom"), nil
	})

	client := New(
		WithProtocol(ProtocolHTTP3),
		WithTransport(custom),
		fixedProxy(&url.URL{Scheme: "http", Host: "proxy.example"}),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "custom", readBody(t, res))
	assert.Zero(t, h3Calls.Load())
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_Do_HTTP3ProxySelectorError(t *testing.T) {
	var h3Calls atomic.Int32

	swapHTTP3Seams(t, countingClient(&h3Calls, "h3"), nil)

	proxyErr := errors.New("proxy selector failed")

	client := New(
		WithProtocol(ProtocolHTTP3),
		WithProxy(func(*http.Request) (*url.URL, error) {
			return nil, proxyErr
		}),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.ErrorIs(t, err, proxyErr)
	assert.Nil(t, res)
	assert.Zero(t, h3Calls.Load())
}

//nolint:paralleltest // substitutes package-level client seams, which are process-wide state.
func TestClient_Do_AutoHTTP3UsesAutoOnProxySelectorError(t *testing.T) {
	var h3Calls, autoCalls atomic.Int32

	swapHTTP3Seams(t, countingClient(&h3Calls, "h3"), countingClient(&autoCalls, "auto"))

	proxyErr := errors.New("proxy selector failed")

	client := New(
		WithProtocol(ProtocolAutoHTTP3),
		WithProxy(func(*http.Request) (*url.URL, error) {
			return nil, proxyErr
		}),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "auto", readBody(t, res))
	assert.Zero(t, h3Calls.Load())
	assert.Equal(t, int32(1), autoCalls.Load())
}

//nolint:paralleltest // The test substitutes the package-level newFallbackClient seam, which is process-wide state.
func TestClient_Do_FallbackJoinsErrors(t *testing.T) {
	original := newFallbackClient

	t.Cleanup(func() {
		newFallbackClient = original
	})

	primaryCause := errors.New("primary cause")
	fallbackCause := errors.New("fallback cause")

	primaryErr := fmt.Errorf(`net/http: HTTP/1.x transport connection broken: malformed HTTP version "HTTP/2": %w`, primaryCause)

	primary := roundTripFunc(func(_ *http.Request) (res *http.Response, err error) {
		err = primaryErr

		return
	})

	newFallbackClient = func(_ *http.Client, _ *tls.Config) (client *http.Client) {
		client = &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (res *http.Response, err error) {
				err = fallbackCause

				return
			}),
		}

		return
	}

	client := New(
		WithHTTPClient(&http.Client{Transport: primary}),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), "GET", "http://example.com/", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.Error(t, err)
	assert.Nil(t, res)
	require.ErrorIs(t, err, primaryCause)
	require.ErrorIs(t, err, fallbackCause)
}

//nolint:paralleltest // The test substitutes the package-level newFallbackClient seam, which is process-wide state.
func TestClient_Do_OtherTransportErrorsDoNotFallBack(t *testing.T) {
	var fallbackCalls atomic.Int32

	original := newFallbackClient

	t.Cleanup(func() {
		newFallbackClient = original
	})

	newFallbackClient = func(_ *http.Client, _ *tls.Config) (client *http.Client) {
		client = &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (res *http.Response, err error) {
				fallbackCalls.Add(1)

				res = newOKResponse("fallback body")

				return
			}),
		}

		return
	}

	errTransport := errors.New("connection refused")

	primary := roundTripFunc(func(_ *http.Request) (res *http.Response, err error) {
		err = errTransport

		return
	})

	client := New(
		WithHTTPClient(&http.Client{Transport: primary}),
		WithRetryMax(0),
	)

	req, err := hqgohttprequest.New(t.Context(), "GET", "http://example.com/", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.ErrorIs(t, err, errTransport)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	assert.Nil(t, res)
	assert.Zero(t, fallbackCalls.Load(), "Expected no fallback for an ordinary transport error")
}

func TestClient_Do_FallbackPreservesCustomClient(t *testing.T) {
	t.Parallel()

	jar, err := cookiejar.New(nil)

	require.NoError(t, err)

	proxyURL := &url.URL{Scheme: "http", Host: "proxy.example"}

	custom := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Jar:     jar,
		Timeout: 5 * time.Second,
	}

	client := New(WithHTTPClient(custom))

	require.NotNil(t, client.fallbackHTTPClient)

	assert.Same(t, custom, client.HTTPClient)
	assert.Equal(t, custom.Timeout, client.fallbackHTTPClient.Timeout)
	assert.Same(t, jar, client.fallbackHTTPClient.Jar)
	require.NotNil(t, client.fallbackHTTPClient.CheckRedirect)
	require.ErrorIs(t, client.fallbackHTTPClient.CheckRedirect(&http.Request{}, nil), http.ErrUseLastResponse)

	fallbackTransport, ok := client.fallbackHTTPClient.Transport.(*http.Transport)

	require.True(t, ok)

	proxy, err := fallbackTransport.Proxy(&http.Request{})

	require.NoError(t, err)
	assert.Equal(t, proxyURL, proxy)
}

//nolint:paralleltest // The test substitutes the package-level newFallbackClient seam, which is process-wide state.
func TestClient_Do_HTTP1BrokenTransportFallsBack(t *testing.T) {
	var primaryCalls, fallbackCalls atomic.Int32

	original := newFallbackClient

	t.Cleanup(func() {
		newFallbackClient = original
	})

	newFallbackClient = func(_ *http.Client, _ *tls.Config) (client *http.Client) {
		client = &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (res *http.Response, err error) {
				fallbackCalls.Add(1)

				res = newOKResponse("fallback body")

				return
			}),
		}

		return
	}

	primary := roundTripFunc(func(_ *http.Request) (res *http.Response, err error) {
		primaryCalls.Add(1)

		err = errHTTP1Broken

		return
	})

	client := New(WithHTTPClient(&http.Client{Transport: primary}))

	req, err := hqgohttprequest.New(t.Context(), "GET", "http://example.com/", nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)

	body, err := io.ReadAll(res.Body)

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, "fallback body", string(body))
	assert.Equal(t, int32(1), primaryCalls.Load(), "Expected the primary client to be attempted once")
	assert.Equal(t, int32(1), fallbackCalls.Load(), "Expected the fallback client to be used once")
}

//nolint:paralleltest // The test substitutes the package-level newFallbackClient seam, which is process-wide state.
func TestClient_Do_FallbackResendsReusableBody(t *testing.T) {
	var primaryCalls, fallbackCalls atomic.Int32

	original := newFallbackClient

	t.Cleanup(func() {
		newFallbackClient = original
	})

	var fallbackBody string

	newFallbackClient = func(_ *http.Client, _ *tls.Config) (client *http.Client) {
		client = &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (res *http.Response, err error) {
				fallbackCalls.Add(1)

				// Like a real transport, consume exactly ContentLength bytes:
				// the reusable body never EOFs.
				body, rerr := io.ReadAll(io.LimitReader(req.Body, req.ContentLength))
				if rerr != nil {
					err = rerr

					return
				}

				fallbackBody = string(body)

				res = newOKResponse("fallback body")

				return
			}),
		}

		return
	}

	primary := roundTripFunc(func(req *http.Request) (res *http.Response, err error) {
		primaryCalls.Add(1)

		// A real transport consumes the body before reporting the failure. The
		// reusable body never EOFs, so consume exactly ContentLength bytes.
		_, _ = io.Copy(io.Discard, io.LimitReader(req.Body, req.ContentLength))

		err = errHTTP1Broken

		return
	})

	client := New(WithHTTPClient(&http.Client{Transport: primary}))

	req, err := hqgohttprequest.New(t.Context(), "PUT", "http://example.com/", "the payload")

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)

	body, err := io.ReadAll(res.Body)

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, "fallback body", string(body))
	assert.Equal(t, "the payload", fallbackBody,
		"Expected the fallback attempt to resend the full body")
	assert.Equal(t, int32(1), primaryCalls.Load())
	assert.Equal(t, int32(1), fallbackCalls.Load())
}

// fixedProxy returns a proxy selector that always returns u.
func fixedProxy(u *url.URL) ClientOptionFunc {
	return WithProxy(func(*http.Request) (*url.URL, error) {
		return u, nil
	})
}

// withoutProxy disables environment proxying for the test client.
func withoutProxy() ClientOptionFunc {
	return WithProxy(func(*http.Request) (*url.URL, error) {
		return nil, nil
	})
}

// swapHTTP3Seams substitutes the package-level HTTP/3 client constructors for
// the duration of a test.
func swapHTTP3Seams(t *testing.T, h3, auto func(*tls.Config) *http.Client) {
	t.Helper()

	originalHTTP3 := newHTTP3Client
	originalAuto := newAutoHTTPClient

	t.Cleanup(func() {
		newHTTP3Client = originalHTTP3
		newAutoHTTPClient = originalAuto
	})

	if h3 != nil {
		newHTTP3Client = h3
	}

	if auto != nil {
		newAutoHTTPClient = auto
	}
}

// countingClient builds a client constructor that records calls and returns a
// fixed body.
func countingClient(counter *atomic.Int32, body string) func(*tls.Config) *http.Client {
	return func(*tls.Config) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				counter.Add(1)

				return newOKResponse(body), nil
			}),
		}
	}
}

func TestClient_RetryErrorBodyReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("body read failed")

	transport := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Header:     make(http.Header),
			Body:       reviewErrorBody{err: readErr},
		}, nil
	})

	client := New(
		WithHTTPClient(&http.Client{Transport: transport}),
		WithRetryMax(0),
		WithRetryErrorBodyLimit(16),
	)

	req, err := hqgohttprequest.New(t.Context(), "GET", "http://example.com/", nil)

	require.NoError(t, err)

	_, err = client.Do(req)

	var retryErr *RetryError

	require.ErrorAs(t, err, &retryErr)
	require.ErrorIs(t, retryErr.BodyErr, readErr)
}
