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
	neturl "net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgohttpuseragent "github.com/hueristiq/hq-lib-http-go/header/useragent"
	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
	hqgohttpstatus "github.com/hueristiq/hq-lib-http-go/status"
)

// readBody reads and closes a wrapped response body, failing the test on
// error.
func readBody(t *testing.T, res *hqgohttp.Response) (body string) {
	t.Helper()

	require.NotNil(t, res)

	body = readRawBody(t, res.Response)

	return
}

// readRawBody reads and closes a raw response body, failing the test on error.
func readRawBody(t *testing.T, res *http.Response) (body string) {
	t.Helper()

	require.NotNil(t, res)

	data, err := io.ReadAll(res.Body)

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	body = string(data)

	return
}

func TestNew(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New()

	require.NotNil(t, client)
}

func TestNew_Options(t *testing.T) {
	t.Parallel()

	t.Run("nil HTTP client is ignored", func(t *testing.T) {
		t.Parallel()

		client := hqgohttp.New(hqgohttp.WithHTTPClient(nil))

		require.NotNil(t, client)
	})

	t.Run("invalid values are normalized", func(t *testing.T) {
		t.Parallel()

		client := hqgohttp.New(
			hqgohttp.WithTimeout(-time.Second),
			hqgohttp.WithRetryPolicy(nil),
			hqgohttp.WithRateLimiter(nil),
			hqgohttp.WithOnRequest(nil),
			hqgohttp.WithOnResponse(nil),
			hqgohttp.WithResponseReadLimit(-1),
		)

		require.NotNil(t, client)
	})
}

func TestClient_ConfigSnapshot(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New(
		hqgohttp.WithTimeout(time.Second),
		hqgohttp.WithHeader("X-Test", "value"),
	)

	config := client.Config()

	assert.Equal(t, time.Second, config.Timeout)
	require.Len(t, config.Headers, 2)

	config.Timeout = 0
	config.Headers[0].Value = "mutated"

	snapshot := client.Config()

	assert.Equal(t, time.Second, snapshot.Timeout,
		"Expected the snapshot to be independent of later mutations")
	assert.NotEqual(t, "mutated", snapshot.Headers[0].Value,
		"Expected the Headers slice to be copied")
}

func TestClient_Do(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("do response"))
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	req, err := hqgohttprequest.New(t.Context(), hqgohttpmethod.GET.String(), server.URL, nil)

	require.NoError(t, err)

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, hqgohttpstatus.OK.Int(), res.StatusCode)
	assert.Equal(t, "do response", readBody(t, res))
}

func TestClient_Do_ClientDefaults(t *testing.T) {
	t.Parallel()

	var mirror requestMirror

	serverURL := newMirrorServer(t, &mirror)

	client := hqgohttp.New(
		hqgohttp.WithHeader("X-Default", "client"),
		hqgohttp.WithHeader("X-Override", "client"),
		hqgohttp.WithHeader("Host", "virtual.example"),
	)

	req, err := hqgohttprequest.New(t.Context(), hqgohttpmethod.GET.String(), serverURL+"/defaults", nil)

	require.NoError(t, err)

	req.Header.Set("X-Override", "request")

	before := req.Header.Clone()

	res, err := client.Do(req)

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, "client", mirror.headers.Get("X-Default"))
	assert.Equal(t, "request", mirror.headers.Get("X-Override"))
	assert.Equal(t, hqgohttp.DefaultUserAgent, mirror.headers.Get("User-Agent"))
	assert.Equal(t, "virtual.example", mirror.host)
	assert.Equal(t, before, req.Header, "Client.Do must not mutate the caller's request headers")
}

func TestClient_Do_SchemeDefaulting(t *testing.T) {
	t.Parallel()

	t.Run("absolute URL", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		req, err := hqgohttprequest.New(t.Context(), hqgohttpmethod.GET.String(), serverURL+"/absolute", nil)

		require.NoError(t, err)

		res, err := client.Do(req)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, "/absolute", mirror.url)
	})

	t.Run("IPv4 host and port", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		parser := hqgohttprequest.URLParserFunc(func(raw string) (*neturl.URL, error) {
			return neturl.Parse("http://" + raw)
		})

		req, err := hqgohttprequest.New(t.Context(), hqgohttpmethod.GET.String(), strings.TrimPrefix(serverURL, "http://")+"/direct", nil, hqgohttprequest.WithURLParser(parser))

		require.NoError(t, err)

		res, err := client.Do(req)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, "/direct", mirror.url)
	})

	t.Run("localhost host and port", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		_, port, err := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
		require.NoError(t, err)

		client := hqgohttp.New()

		parser := hqgohttprequest.URLParserFunc(func(raw string) (*neturl.URL, error) {
			return neturl.Parse("http://" + raw)
		})

		req, err := hqgohttprequest.New(t.Context(), hqgohttpmethod.GET.String(), "localhost:"+port+"/direct", nil, hqgohttprequest.WithURLParser(parser))

		require.NoError(t, err)

		res, err := client.Do(req)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, "/direct", mirror.url)
	})
}

func TestClient_Do_NilRequest(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New()

	res, err := client.Do(nil)

	require.Error(t, err)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "http.Client.Do:")
}

func TestClient_NilContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("nil ctx"))
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "nil ctx", readBody(t, res))
}

func TestClient_ConvenienceMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//nolint:gosec // Echoing the method and path in a test server is not an XSS vector.
		_, _ = fmt.Fprintf(w, "%s %s", r.Method, r.URL.Path)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	tests := []struct {
		name     string
		call     func(rawURL string) (*hqgohttp.Response, error)
		expected string
	}{
		{
			name: "Get",
			call: func(rawURL string) (*hqgohttp.Response, error) {
				return client.Get(rawURL)
			},
			expected: "GET /get",
		},
		{
			name: "Head",
			call: func(rawURL string) (*hqgohttp.Response, error) {
				return client.Head(rawURL)
			},
			expected: "", // HEAD responses carry no body.
		},
		{
			name: "Post",
			call: func(rawURL string) (*hqgohttp.Response, error) {
				return client.Post(rawURL, "payload")
			},
			expected: "POST /post",
		},
		{
			name: "Query",
			call: func(rawURL string) (*hqgohttp.Response, error) {
				return client.Query(rawURL, "payload")
			},
			expected: "QUERY /query",
		},
		{
			name: "Put",
			call: func(rawURL string) (*hqgohttp.Response, error) {
				return client.Put(rawURL, "payload")
			},
			expected: "PUT /put",
		},
		{
			name: "Patch",
			call: func(rawURL string) (*hqgohttp.Response, error) {
				return client.Patch(rawURL, "payload")
			},
			expected: "PATCH /patch",
		},
		{
			name: "Delete",
			call: func(rawURL string) (*hqgohttp.Response, error) {
				return client.Delete(rawURL)
			},
			expected: "DELETE /delete",
		},
		{
			name: "Options",
			call: func(rawURL string) (*hqgohttp.Response, error) {
				return client.Options(rawURL)
			},
			expected: "OPTIONS /options",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := "/" + strings.ToLower(tt.name)

			res, err := tt.call(server.URL + path)

			require.NoError(t, err)
			require.NotNil(t, res)

			assert.Equal(t, tt.expected, readBody(t, res))
		})
	}
}

// fakeRateLimiter is a RateLimiter counting its invocations, optionally
// failing.
//
// Fields:
//   - calls (*atomic.Int32): the number of Wait invocations.
//   - err (error): the error Wait returns; nil means success.
//   - onWait (func()): an optional callback run by Wait before returning.
type fakeRateLimiter struct {
	calls  *atomic.Int32
	err    error
	onWait func()
}

// Compile-time guard ensuring [fakeRateLimiter] satisfies
// [hqgohttp.RateLimiter].
var _ hqgohttp.RateLimiter = (*fakeRateLimiter)(nil)

// Wait counts the invocation and returns the configured error.
func (l *fakeRateLimiter) Wait(_ context.Context) (err error) {
	l.calls.Add(1)

	if l.onWait != nil {
		l.onWait()
	}

	err = l.err

	return
}

// closeCountingTransport is an http.RoundTripper answering 200 OK and
// counting CloseIdleConnections calls.
type closeCountingTransport struct {
	closes *atomic.Int32
}

// RoundTrip answers every request with a 200 OK.
func (t *closeCountingTransport) RoundTrip(_ *http.Request) (res *http.Response, err error) {
	res = &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("OK")),
		Header:     make(http.Header),
	}

	return
}

// CloseIdleConnections increments the close counter.
func (t *closeCountingTransport) CloseIdleConnections() {
	t.closes.Add(1)
}

func TestClient_UserAgent(t *testing.T) {
	t.Parallel()

	// newCaptureServer starts an httptest server recording the User-Agent of
	// the last request it received, and returns its URL and the record.
	newCaptureServer := func(t *testing.T) (serverURL string, captured *atomic.Value) {
		t.Helper()

		captured = &atomic.Value{}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured.Store(r.UserAgent())

			_, _ = w.Write([]byte("ua"))
		}))

		t.Cleanup(server.Close)

		serverURL = server.URL

		return
	}

	t.Run("default", func(t *testing.T) {
		t.Parallel()

		serverURL, captured := newCaptureServer(t)

		client := hqgohttp.New()

		res, err := client.Get(serverURL)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
		assert.Equal(t, hqgohttp.DefaultUserAgent, captured.Load())
	})

	t.Run("client header", func(t *testing.T) {
		t.Parallel()

		serverURL, captured := newCaptureServer(t)

		client := hqgohttp.New(hqgohttp.WithHeader("user-agent", "custom-agent/1.0"))

		res, err := client.Get(serverURL)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
		assert.Equal(t, "custom-agent/1.0", captured.Load())
	})

	t.Run("generated value as a client header", func(t *testing.T) {
		t.Parallel()

		serverURL, captured := newCaptureServer(t)

		userAgent := hqgohttpuseragent.Random()

		client := hqgohttp.New(hqgohttp.WithHeader("User-Agent", userAgent))

		res, err := client.Get(serverURL)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
		assert.Equal(t, userAgent, captured.Load())
	})

	t.Run("request header wins", func(t *testing.T) {
		t.Parallel()

		serverURL, captured := newCaptureServer(t)

		client := hqgohttp.New(hqgohttp.WithHeader("User-Agent", "custom-agent/1.0"))

		res, err := client.Request().
			URL(serverURL).
			SetHeader("User-Agent", "explicit-agent/2.0").
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
		assert.Equal(t, "explicit-agent/2.0", captured.Load())
	})

	t.Run("client header wins over the default", func(t *testing.T) {
		t.Parallel()

		serverURL, captured := newCaptureServer(t)

		client := hqgohttp.New(hqgohttp.WithHeader("User-Agent", "header-agent/1.0"))

		res, err := client.Get(serverURL)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
		assert.Equal(t, "header-agent/1.0", captured.Load())
	})
}

// reviewRoundTripFunc adapts a function to [http.RoundTripper].
type reviewRoundTripFunc func(req *http.Request) (res *http.Response, err error)

// RoundTrip calls the function.
func (f reviewRoundTripFunc) RoundTrip(req *http.Request) (res *http.Response, err error) {
	return f(req)
}

func TestClient_DoReturnsWrappedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "wrapped")
	}))

	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)

	require.NoError(t, err)

	res, err := hqgohttp.New().Do(&hqgohttprequest.Request{Request: req})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Response)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "wrapped", readBody(t, res))
}

func TestClient_Std(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = io.WriteString(w, "ok")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
	)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)

	require.NoError(t, err)

	res, err := client.Std().Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "ok", readRawBody(t, res))
	assert.Equal(t, int32(2), attempts.Load())
}

func TestClient_Std_POSTDoesNotRetryByDefault(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
	)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader("payload"))

	require.NoError(t, err)

	req.Header.Set("Content-Type", "text/plain")

	res, err := client.Std().Do(req)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.Equal(t, "boom", readRawBody(t, res))
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClient_RoundTrip(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "round trip")
	}))

	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)

	require.NoError(t, err)

	client := hqgohttp.New()

	res, err := client.RoundTrip(req)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "round trip", readRawBody(t, res))
}

func TestClient_RoundTripNilRequest(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New()

	res, err := client.RoundTrip(nil) //nolint:bodyclose // The request is rejected before a response exists.

	require.ErrorIs(t, err, hqgohttp.ErrNilRequest)
	assert.Nil(t, res)

	res, err = client.RoundTrip(&http.Request{}) //nolint:bodyclose // The request is rejected before a response exists.

	require.ErrorIs(t, err, hqgohttp.ErrNilRequestURL)
	assert.Nil(t, res)
}

func TestClient_PrepareRetry(t *testing.T) {
	t.Parallel()

	var (
		attempts  atomic.Int32
		sawSigned atomic.Bool
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		if r.Header.Get("X-Signature") == "signed-2" {
			sawSigned.Store(true)
		}

		w.WriteHeader(http.StatusOK)
	}))

	t.Cleanup(server.Close)

	var hookAttempts []int

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
		hqgohttp.WithPrepareRetry(func(_ context.Context, req *http.Request, attempt int) error {
			hookAttempts = append(hookAttempts, attempt)

			if attempt == 2 {
				req.Header.Set("X-Signature", "signed-2")
			}

			return nil
		}),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, []int{2}, hookAttempts, "Expected the hook to run only before the retry")
	assert.True(t, sawSigned.Load(), "Expected the re-signed header on the retried request")
	assert.Equal(t, int32(2), attempts.Load())
	require.NoError(t, res.Body.Close())
}

func TestClient_PrepareRetryAborts(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	t.Cleanup(server.Close)

	signingErr := errors.New("signing failed")

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		hqgohttp.WithPrepareRetry(func(context.Context, *http.Request, int) error {
			return signingErr
		}),
	)

	res, err := client.Get(server.URL)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.ErrorIs(t, err, signingErr)
	assert.Nil(t, res)
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClient_RequestPrepareRetryOverride(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
		hqgohttp.WithPrepareRetry(func(context.Context, *http.Request, int) error {
			return errors.New("client hook must be overridden")
		}),
	)

	res, err := client.Get(server.URL, hqgohttp.WithRequestPrepareRetry(func(context.Context, *http.Request, int) error {
		return nil
	}))

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, int32(2), attempts.Load())
	require.NoError(t, res.Body.Close())
}

func TestClient_Do_ResponseBodyOutlivesExecution(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		w.(http.Flusher).Flush()

		// Hold the body back until well after Do has returned, so the caller's
		// read below is in flight across Do's deferred cleanup: if Do cancels
		// the timeout context on return, the read aborts with context canceled.
		time.Sleep(200 * time.Millisecond)

		_, _ = io.WriteString(w, "streamed body")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithTimeout(30 * time.Second))

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	body, err := io.ReadAll(res.Body)

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, "streamed body", string(body))
}

func TestClient_NoExecutionTimeoutByDefault(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)

		_, _ = io.WriteString(w, "done")
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "done", readBody(t, res))
	require.NoError(t, res.Body.Close())
}

func TestClient_ResponseBodyCloseCancelsContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("body"))
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithTimeout(time.Hour))

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	responseCtx := res.Request.Context()

	require.NoError(t, context.Cause(responseCtx))

	require.NoError(t, res.Body.Close())

	require.ErrorIs(t, context.Cause(responseCtx), context.Canceled,
		"Expected closing the body to release the request context")
}

func TestClient_AttemptTimeout(t *testing.T) {
	t.Parallel()

	t.Run("times out and retries", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				time.Sleep(200 * time.Millisecond)
			}

			_, _ = w.Write([]byte("ok"))
		}))

		t.Cleanup(server.Close)

		client := hqgohttp.New(
			hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
			hqgohttp.WithAttemptTimeout(50*time.Millisecond),
			hqgohttp.WithTimeout(2*time.Second),
		)

		res, err := client.Get(server.URL)

		require.NoError(t, err)
		assert.Equal(t, "ok", readBody(t, res))
		assert.GreaterOrEqual(t, calls.Load(), int32(2))
	})

	t.Run("does not bound body reads", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)

			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}

			time.Sleep(300 * time.Millisecond)

			_, _ = io.WriteString(w, "slow body")
		}))

		t.Cleanup(server.Close)

		client := hqgohttp.New(
			hqgohttp.WithAttemptTimeout(150*time.Millisecond),
			hqgohttp.WithTimeout(2*time.Second),
		)

		res, err := client.Get(server.URL)

		require.NoError(t, err)
		assert.Equal(t, "slow body", readBody(t, res))
	})

	t.Run("overall timeout wins", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(500 * time.Millisecond)

			_, _ = w.Write([]byte("late"))
		}))

		t.Cleanup(server.Close)

		client := hqgohttp.New(
			hqgohttp.WithAttemptTimeout(time.Second),
			hqgohttp.WithTimeout(100*time.Millisecond),
		)

		res, err := client.Get(server.URL)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("request override disables the attempt bound", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)

			_, _ = w.Write([]byte("slow but allowed"))
		}))

		t.Cleanup(server.Close)

		client := hqgohttp.New(
			hqgohttp.WithAttemptTimeout(25*time.Millisecond),
			hqgohttp.WithTimeout(2*time.Second),
		)

		res, err := client.Get(server.URL, hqgohttp.WithRequestAttemptTimeout(0))

		require.NoError(t, err)
		assert.Equal(t, "slow but allowed", readBody(t, res))
	})
}

func TestClient_ResponseBodyTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		w.(http.Flusher).Flush()

		time.Sleep(300 * time.Millisecond)

		_, _ = io.WriteString(w, "too late")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithTimeout(time.Second),
		hqgohttp.WithResponseBodyTimeout(50*time.Millisecond),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	start := time.Now()

	_, err = io.ReadAll(res.Body)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second,
		"Expected the body timeout to end the read promptly")

	require.NoError(t, res.Body.Close())
}

func TestClient_Timeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithTimeout(150 * time.Millisecond),
	)

	start := time.Now()

	res, err := client.Get(server.URL)

	elapsed := time.Since(start)

	require.Error(t, err)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	assert.Nil(t, res)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 10*time.Second,
		"Expected the configured timeout to bound the execution")
}

func TestClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	requestReceived := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case requestReceived <- struct{}{}:
		default:
		}

		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithTimeout(30 * time.Second))

	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	go func() {
		select {
		case <-requestReceived:
		case <-t.Context().Done():
			return
		}

		cancel()
	}()

	start := time.Now()

	res, err := client.Request().Context(ctx).URL(server.URL).Do()

	elapsed := time.Since(start)

	require.Error(t, err)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	assert.Nil(t, res)
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 10*time.Second,
		"Expected the cancellation to end the execution promptly")
}

func TestClient_ExecutionTimeoutDoesNotBoundBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		w.(http.Flusher).Flush()

		time.Sleep(300 * time.Millisecond)

		_, _ = io.WriteString(w, "streamed after the execution timeout")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithTimeout(100*time.Millisecond),
		hqgohttp.WithResponseBodyTimeout(0),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	require.NoError(t, context.Cause(res.Request.Context()),
		"Expected the execution timer to stop after the response headers")

	body, err := io.ReadAll(res.Body)

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, "streamed after the execution timeout", string(body))
}

func TestClient_NoBodyResponseCancelsContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithTimeout(time.Hour))

	res, err := client.Head(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, http.NoBody, res.Std().Body) //nolint:bodyclose // res.Body.Close closes the same underlying body.
	require.ErrorIs(t, context.Cause(res.Request.Context()), context.Canceled,
		"Expected a bodyless response to release the request context immediately")

	require.NoError(t, res.Body.Close())
}

func TestClient_RateLimiter(t *testing.T) {
	t.Parallel()

	t.Run("invoked before every attempt", func(t *testing.T) {
		t.Parallel()

		var attempts atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) < 3 {
				w.WriteHeader(hqgohttpstatus.InternalServerError.Int())

				return
			}

			_, _ = w.Write([]byte("limited"))
		}))

		t.Cleanup(server.Close)

		var calls atomic.Int32

		limiter := &fakeRateLimiter{calls: &calls}

		client := hqgohttp.New(
			hqgohttp.WithRateLimiter(limiter),
			hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		)

		res, err := client.Get(server.URL)

		require.NoError(t, err)
		require.NotNil(t, res)

		assert.Equal(t, "limited", readBody(t, res))
		assert.Equal(t, int32(3), calls.Load(),
			"Expected the limiter to be waited on once per attempt")
	})

	t.Run("failure aborts the execution", func(t *testing.T) {
		t.Parallel()

		errLimit := errors.New("limiter unavailable")

		var calls atomic.Int32

		limiter := &fakeRateLimiter{calls: &calls, err: errLimit}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("unreachable"))
		}))

		t.Cleanup(server.Close)

		client := hqgohttp.New(hqgohttp.WithRateLimiter(limiter))

		res, err := client.Get(server.URL)

		require.Error(t, err)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		assert.Nil(t, res)
		require.ErrorIs(t, err, errLimit)
		assert.Equal(t, int32(1), calls.Load(),
			"Expected the limiter failure to abort before any retry")
	})

	t.Run("context error wins over the limiter failure", func(t *testing.T) {
		t.Parallel()

		errLimit := errors.New("limiter unavailable")

		ctx, cancel := context.WithCancel(t.Context())

		defer cancel()

		var calls atomic.Int32

		limiter := &fakeRateLimiter{calls: &calls, err: errLimit, onWait: cancel}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("unreachable"))
		}))

		t.Cleanup(server.Close)

		client := hqgohttp.New(hqgohttp.WithRateLimiter(limiter))

		res, err := client.Request().Context(ctx).URL(server.URL).Do()

		require.Error(t, err)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		assert.Nil(t, res)
		require.ErrorIs(t, err, context.Canceled)
		require.NotErrorIs(t, err, errLimit)
		assert.Equal(t, int32(1), calls.Load(),
			"Expected the limiter failure to abort before any retry")
	})
}

func TestClient_OnRequestAndOnResponsePerRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))

	t.Cleanup(server.Close)

	var requestAttempts, responseAttempts atomic.Int32

	client := hqgohttp.New()

	res, err := client.Get(server.URL,
		hqgohttp.WithRequestOnRequest(func(event hqgohttp.RequestEvent) {
			assert.Equal(t, 1, event.Attempt)
			assert.NotNil(t, event.Request)
			requestAttempts.Add(1)
		}),
		hqgohttp.WithRequestOnResponse(func(event hqgohttp.ResponseEvent) {
			assert.Equal(t, 1, event.Attempt)
			assert.NotNil(t, event.Response)
			responseAttempts.Add(1)
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))
	assert.Equal(t, int32(1), requestAttempts.Load())
	assert.Equal(t, int32(1), responseAttempts.Load())
}

func TestClient_ObservabilityCallbacks(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(hqgohttpstatus.InternalServerError.Int())

			return
		}

		_, _ = w.Write([]byte("observed"))
	}))

	t.Cleanup(server.Close)

	var onRequestCalls, onResponseCalls atomic.Int32

	var (
		requestAttempts  []int
		responseAttempts []int
		statuses         []int
	)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		hqgohttp.WithOnRequest(func(event hqgohttp.RequestEvent) {
			assert.NotNil(t, event.Request)

			requestAttempts = append(requestAttempts, event.Attempt)

			onRequestCalls.Add(1)
		}),
		hqgohttp.WithOnResponse(func(event hqgohttp.ResponseEvent) {
			onResponseCalls.Add(1)

			responseAttempts = append(responseAttempts, event.Attempt)

			if event.Response != nil {
				statuses = append(statuses, event.Response.StatusCode)
			}
		}),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "observed", readBody(t, res))
	assert.Equal(t, int32(2), onRequestCalls.Load(),
		"Expected the request callback once per attempt")
	assert.Equal(t, int32(2), onResponseCalls.Load(),
		"Expected the response callback once per attempt")
	assert.Equal(t, []int{1, 2}, requestAttempts,
		"Expected the request callback to receive the attempt number")
	assert.Equal(t, []int{1, 2}, responseAttempts,
		"Expected the response callback to receive the attempt number")
	assert.Equal(t, []int{
		hqgohttpstatus.InternalServerError.Int(),
		hqgohttpstatus.OK.Int(),
	}, statuses, "Expected the callback to observe every attempt's outcome")

	// Attempt durations are not asserted positive: the Windows monotonic clock
	// advances at the timer interrupt (15.625 ms by default), so a fast local
	// attempt can measure as 0s.
}

func TestClient_MaxRequestsBeforeIdleClose(t *testing.T) {
	t.Parallel()

	var closes atomic.Int32

	transport := &closeCountingTransport{closes: &closes}

	client := hqgohttp.New(
		hqgohttp.WithHTTPClient(&http.Client{Transport: transport}),
		hqgohttp.WithMaxRequestsBeforeIdleClose(2),
	)

	for range 5 {
		res, err := client.Get("http://example.com/")

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
	}

	assert.Equal(t, int32(2), closes.Load(),
		"Expected idle connections to be closed on the 2nd and 4th completed requests")
}

func TestClient_RetryExhaustionHandlerReturnsFinalResponse(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "server error body")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
		hqgohttp.WithOnRetryExhaustion(func(resp *http.Response, err error, attemptCount int) (*http.Response, error) {
			require.NotNil(t, resp)
			require.Error(t, err)
			assert.Equal(t, 2, attemptCount)

			return resp, nil
		}),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.Equal(t, "server error body", readBody(t, res))
	assert.Equal(t, int32(2), attempts.Load())
}

func TestClient_RetryExhaustionHandlerReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	t.Cleanup(server.Close)

	handledErr := errors.New("handled by the exhaustion handler")

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
		hqgohttp.WithOnRetryExhaustion(func(*http.Response, error, int) (*http.Response, error) {
			return nil, handledErr
		}),
	)

	res, err := client.Get(server.URL)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.ErrorIs(t, err, handledErr)
	assert.Nil(t, res)
}

func TestClient_RetryExhaustionHandlerWithNoRetries(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "no retries")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithOnRetryExhaustion(func(resp *http.Response, _ error, attemptCount int) (*http.Response, error) {
			assert.Equal(t, 1, attemptCount)

			return resp, nil
		}),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.Equal(t, "no retries", readBody(t, res))
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClient_RetryExhaustionHandlerPerRequestOverride(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "per request")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
	)

	res, err := client.Get(server.URL,
		hqgohttp.WithRequestOnRetryExhaustion(func(resp *http.Response, _ error, _ int) (*http.Response, error) {
			return resp, nil
		}),
	)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.Equal(t, "per request", readBody(t, res))
}

func TestClient_RetryExhaustionHandlerNotCalledOnSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))

	t.Cleanup(server.Close)

	var called atomic.Bool

	client := hqgohttp.New(
		hqgohttp.WithOnRetryExhaustion(func(resp *http.Response, _ error, _ int) (*http.Response, error) {
			called.Store(true)

			return resp, nil
		}),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))
	assert.False(t, called.Load())
}

func TestClient_RetryExhaustionHandlerWithNonReplayableBody(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))

	t.Cleanup(server.Close)

	plain, err := http.NewRequestWithContext(t.Context(), http.MethodPut, server.URL, io.NopCloser(strings.NewReader("payload")))

	require.NoError(t, err)

	plain.GetBody = nil
	plain.ContentLength = int64(len("payload"))

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		hqgohttp.WithOnRetryExhaustion(func(resp *http.Response, _ error, _ int) (*http.Response, error) {
			return resp, nil
		}),
	)

	res, err := client.Do(&hqgohttprequest.Request{Request: plain})

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.Equal(t, "boom", readBody(t, res))
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClient_Query(t *testing.T) {
	t.Parallel()

	const payload = "query payload"

	var (
		method atomic.Value
		body   atomic.Value
		length atomic.Int64
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)

		method.Store(r.Method)
		body.Store(string(data))
		length.Store(r.ContentLength)

		_, _ = io.WriteString(w, "ok")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	res, err := client.Query(server.URL, payload)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "ok", readBody(t, res))
	assert.Equal(t, "QUERY", method.Load())
	assert.Equal(t, payload, body.Load())
	assert.Equal(t, int64(len(payload)), length.Load())
}

func TestClient_CloseRejectsFutureRequests(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New()

	client.Close()

	res, err := client.Get("https://example.com")

	require.ErrorIs(t, err, hqgohttp.ErrClientClosed)
	assert.Nil(t, res)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", http.NoBody)

	require.NoError(t, err)

	raw, err := client.RoundTrip(req) //nolint:bodyclose // The request is rejected before a response exists.

	require.ErrorIs(t, err, hqgohttp.ErrClientClosed)
	assert.Nil(t, raw)

	stdReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", http.NoBody)

	require.NoError(t, err)

	stdRes, err := client.Std().Do(stdReq) //nolint:bodyclose // The request is rejected before a response exists.

	require.ErrorIs(t, err, hqgohttp.ErrClientClosed)
	assert.Nil(t, stdRes)

	client.CloseIdleConnections()
}

func TestClient_CloseCancelsInFlight(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	type result struct {
		res *hqgohttp.Response
		err error
	}

	results := make(chan result, 1)

	go func() {
		res, err := client.Get(server.URL)
		results <- result{res: res, err: err}
	}()

	<-started
	client.Close()

	select {
	case got := <-results:
		require.ErrorIs(t, got.err, hqgohttp.ErrClientClosed)
		assert.Nil(t, got.res)
	case <-time.After(5 * time.Second):
		t.Fatal("request did not stop after Close")
	}
}

func TestClient_CloseCancelsBodyRead(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		close(started)
		<-r.Context().Done()
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	<-started
	client.Close()

	_, err = res.Body.Bytes()

	require.ErrorIs(t, err, hqgohttp.ErrClientClosed)
}

func TestClient_CloseIdempotent(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New()

	var waitGroup sync.WaitGroup

	for range 8 {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			client.Close()
		}()
	}

	waitGroup.Wait()
	client.Close()

	_, err := client.Get("https://example.com")

	require.ErrorIs(t, err, hqgohttp.ErrClientClosed)
}

func TestClient_CloseIdleConnections(t *testing.T) {
	t.Parallel()

	var closes atomic.Int32

	transport := &closeCountingTransport{closes: &closes}

	client := hqgohttp.New(hqgohttp.WithHTTPClient(&http.Client{Transport: transport}))

	client.CloseIdleConnections()

	assert.Equal(t, int32(1), closes.Load(),
		"Expected CloseIdleConnections to reach the primary client's transport")
}

func TestClient_NilTransportClose(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New(hqgohttp.WithHTTPClient(&http.Client{}))

	require.NotPanics(t, client.CloseIdleConnections)
	require.NotPanics(t, client.Close)
}

func TestClient_NonReplayableBodyStopsWithOriginalOutcome(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))

	t.Cleanup(server.Close)

	plain, err := http.NewRequestWithContext(t.Context(), http.MethodPut, server.URL, io.NopCloser(strings.NewReader("payload")))

	require.NoError(t, err)

	plain.GetBody = nil
	plain.ContentLength = int64(len("payload"))

	req := &hqgohttprequest.Request{Request: plain}

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		hqgohttp.WithRetryErrorBodyLimit(hqgohttp.RecommendedRetryErrorBodyLimit),
	)

	res, err := client.Do(req)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	require.ErrorIs(t, err, hqgohttp.ErrBodyNotReplayable)
	require.NotErrorIs(t, err, hqgohttp.ErrRetriesExhausted)
	assert.Equal(t, http.StatusInternalServerError, retryErr.StatusCode)
	assert.Equal(t, "boom", string(retryErr.Body))
	assert.Equal(t, 1, retryErr.Attempts)
	assert.Equal(t, int32(1), attempts.Load(),
		"Expected the unreplayable body to stop before another attempt")
}

func TestClient_ReplayabilityGetBodyFailure(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))

	t.Cleanup(server.Close)

	plain, err := http.NewRequestWithContext(t.Context(), http.MethodPut, server.URL, strings.NewReader("payload"))

	require.NoError(t, err)

	plain.GetBody = func() (io.ReadCloser, error) {
		return nil, errors.New("get body failed")
	}

	req := &hqgohttprequest.Request{Request: plain}

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		hqgohttp.WithRetryErrorBodyLimit(hqgohttp.RecommendedRetryErrorBodyLimit),
	)

	res, err := client.Do(req)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, http.StatusInternalServerError, retryErr.StatusCode)
	assert.Equal(t, "boom", string(retryErr.Body))
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClient_GetBodyIsRewoundBeforeNextAttempt(t *testing.T) {
	t.Parallel()

	var (
		getBodyCalls atomic.Int32
		calls        atomic.Int32
		got          string
	)

	plain, err := http.NewRequestWithContext(t.Context(), http.MethodPut, "http://example.com/", strings.NewReader("payload"))

	require.NoError(t, err)

	originalGetBody := plain.GetBody
	plain.GetBody = func() (io.ReadCloser, error) {
		getBodyCalls.Add(1)

		return originalGetBody()
	}

	transport := reviewRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, rerr := io.ReadAll(req.Body)
		if rerr != nil {
			return nil, rerr
		}

		if calls.Add(1) == 1 {
			return nil, errors.New("transient failure")
		}

		got = string(body)

		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})

	client := hqgohttp.New(
		hqgohttp.WithHTTPClient(&http.Client{Transport: transport}),
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
	)

	res, err := client.Do(&hqgohttprequest.Request{Request: plain})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, "payload", got, "Expected the retry to resend the full body")
	assert.Equal(t, int32(1), getBodyCalls.Load(),
		"Expected the body to be refetched once, before the retry")
}

func TestClient_ReusableBodyAfterPartialRead(t *testing.T) {
	t.Parallel()

	const payload = "ABCDEFGHIJ"

	var (
		calls atomic.Int32
		got   string
	)

	transport := reviewRoundTripFunc(func(req *http.Request) (res *http.Response, err error) {
		if calls.Add(1) == 1 {
			buf := make([]byte, 4)

			_, _ = io.ReadFull(req.Body, buf)

			return nil, errors.New("transient connection failure")
		}

		buf := make([]byte, req.ContentLength)

		_, err = io.ReadFull(req.Body, buf)
		if err != nil {
			return nil, err
		}

		got = string(buf)

		return &http.Response{
			Status:     "200 OK",
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})

	client := hqgohttp.New(
		hqgohttp.WithHTTPClient(&http.Client{Transport: transport}),
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
	)

	res, err := client.Put("http://example.invalid/", payload)

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, payload, got,
		"Expected the retry to resend the entire body, not the remainder")
}

func TestClient_PostEmptyBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		_, _ = w.Write(body)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	for name, body := range map[string]any{
		"empty string": "",
		"empty []byte": []byte{},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			start := time.Now()

			res, err := client.Post(server.URL, body)

			elapsed := time.Since(start)

			require.NoError(t, err)
			require.NotNil(t, res)

			assert.Empty(t, readBody(t, res))
			assert.Less(t, elapsed, 10*time.Second,
				"Expected the empty-body request to complete promptly")
		})
	}
}

func TestClient_Redirect307ReplaysBody(t *testing.T) {
	t.Parallel()

	const payload = "redirected payload"

	var (
		hit  atomic.Bool
		body atomic.Value
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err == nil {
			body.Store(string(data))
		}

		hit.Store(true)

		_, _ = w.Write([]byte("ok"))
	})

	server := httptest.NewServer(mux)

	t.Cleanup(server.Close)

	client := hqgohttp.New()

	res, err := client.Post(server.URL+"/start", payload)

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.True(t, hit.Load(), "Expected the redirect target to be reached")

	redirectedBody, _ := body.Load().(string)

	assert.Equal(t, payload, redirectedBody)
}

func TestClient_ReusableBodyAcrossRetries(t *testing.T) {
	t.Parallel()

	var bodies []string

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			bodies = append(bodies, string(body))
		}

		if attempts.Add(1) == 1 {
			w.WriteHeader(hqgohttpstatus.InternalServerError.Int())

			return
		}

		_, _ = w.Write([]byte("got it"))
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 2}),
	)

	res, err := client.Put(server.URL, "the payload")

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "got it", readBody(t, res))
	assert.Equal(t, []string{"the payload", "the payload"}, bodies,
		"Expected every attempt to resend the full body")
}

func TestClient_NonIdempotentNotRetriedByDefault(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
	)

	res, err := client.Post(server.URL, "payload")

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.Equal(t, int32(1), attempts.Load(),
		"Expected a POST not to be retried by default")

	require.NoError(t, res.Body.Close())
}

func TestClient_NonIdempotentRetriesOptOut(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetryIdempotent(false),
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
	)

	res, err := client.Post(server.URL, "payload")

	require.Error(t, err)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, 3, retryErr.Attempts)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestClient_RedirectPolicyErrorIsFinal(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)

		if r.URL.Path == "/" {
			http.Redirect(w, r, "/next", http.StatusFound)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	t.Cleanup(server.Close)

	policyErr := errors.New("redirect refused by policy")

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		hqgohttp.WithCheckRedirect(func(*http.Request, []*http.Request) error {
			return policyErr
		}),
	)

	res, err := client.Get(server.URL)

	require.Error(t, err)
	require.ErrorContains(t, err, policyErr.Error())
	assert.Equal(t, int32(1), hits.Load(),
		"Expected a redirect-policy abort not to be retried")

	if res != nil {
		require.NoError(t, res.Body.Close())
	}
}

func TestClient_RetryPolicySeesResponse(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(hqgohttpstatus.InternalServerError.Int())

			return
		}

		_, _ = w.Write([]byte("recovered"))
	}))

	t.Cleanup(server.Close)

	var (
		statuses       []int
		methods        []string
		attemptNumbers []int
	)

	policy := func(_ context.Context, req *http.Request, resp *http.Response, _ error, attempt int) (retry bool, rerr error) {
		methods = append(methods, req.Method)
		attemptNumbers = append(attemptNumbers, attempt)

		if resp != nil {
			statuses = append(statuses, resp.StatusCode)
		}

		if resp != nil && resp.StatusCode == hqgohttpstatus.InternalServerError.Int() {
			retry = true
		}

		return
	}

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		hqgohttp.WithRetryPolicy(policy),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "recovered", readBody(t, res))
	assert.Equal(t, []int{
		hqgohttpstatus.InternalServerError.Int(),
		hqgohttpstatus.OK.Int(),
	}, statuses, "Expected the policy to observe every attempt's response")
	assert.Equal(t, []string{http.MethodGet, http.MethodGet}, methods)
	assert.Equal(t, []int{1, 2}, attemptNumbers)
}

func TestClient_OnRetryRequestOverride(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = io.WriteString(w, "recovered")
	}))

	t.Cleanup(server.Close)

	var clientCalls, requestCalls atomic.Int32

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		hqgohttp.WithOnRetry(func(_ hqgohttp.RetryEvent) {
			clientCalls.Add(1)
		}),
	)

	res, err := client.Get(server.URL, hqgohttp.WithRequestOnRetry(func(_ hqgohttp.RetryEvent) {
		requestCalls.Add(1)
	}))

	require.NoError(t, err)
	assert.Equal(t, "recovered", readBody(t, res))
	assert.Zero(t, clientCalls.Load(), "Expected the request hook to replace the client hook")
	assert.Equal(t, int32(1), requestCalls.Load())
}

func TestClient_RequestOptionOverrides(t *testing.T) {
	t.Parallel()

	// newUnavailableServer starts an httptest server that always answers 503
	// and counts its hits.
	newUnavailableServer := func(t *testing.T) (serverURL string, attempts *atomic.Int32) {
		t.Helper()

		attempts = &atomic.Int32{}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)

			w.WriteHeader(hqgohttpstatus.ServiceUnavailable.Int())
		}))

		t.Cleanup(server.Close)

		serverURL = server.URL

		return
	}

	t.Run("retrier override", func(t *testing.T) {
		t.Parallel()

		serverURL, attempts := newUnavailableServer(t)

		client := hqgohttp.New(
			hqgohttp.WithRetrier(testRetrier{maxAttempts: 6}),
		)

		res, err := client.Get(serverURL, hqgohttp.WithRequestRetrier(testRetrier{maxAttempts: 2}))

		require.Error(t, err)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		assert.Nil(t, res)
		assert.Equal(t, int32(2), attempts.Load(),
			"Expected the per-request override to win over the client configuration")
	})

	t.Run("nil disables retrying", func(t *testing.T) {
		t.Parallel()

		serverURL, attempts := newUnavailableServer(t)

		client := hqgohttp.New(
			hqgohttp.WithRetrier(testRetrier{maxAttempts: 6}),
		)

		res, err := client.Get(serverURL, hqgohttp.WithRequestRetrier(nil))

		require.Error(t, err)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		assert.Nil(t, res)
		assert.Equal(t, int32(1), attempts.Load())
	})
}

func TestClient_OnRetry(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = io.WriteString(w, "recovered")
	}))

	t.Cleanup(server.Close)

	var events []hqgohttp.RetryEvent

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
		hqgohttp.WithOnRetry(func(event hqgohttp.RetryEvent) {
			events = append(events, event)
		}),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	assert.Equal(t, "recovered", readBody(t, res))
	require.Len(t, events, 1)

	event := events[0]

	assert.Equal(t, 1, event.Attempt)
	require.NotNil(t, event.Request)
	assert.Equal(t, http.MethodGet, event.Request.Method)
	require.NotNil(t, event.Response)
	assert.Equal(t, http.StatusInternalServerError, event.Response.StatusCode)
	require.Error(t, event.Err)
	assert.Positive(t, event.Wait)
}

func TestClient_RetryPolicyAbort(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(hqgohttpstatus.Teapot.Int())

		_, _ = w.Write([]byte("short and stout"))
	}))

	t.Cleanup(server.Close)

	errTeapot := errors.New("teapot detected")

	policy := func(_ context.Context, _ *http.Request, resp *http.Response, _ error, _ int) (retry bool, rerr error) {
		if resp != nil && resp.StatusCode == hqgohttpstatus.Teapot.Int() {
			rerr = errTeapot
		}

		return
	}

	client := hqgohttp.New(hqgohttp.WithRetryPolicy(policy))

	res, err := client.Get(server.URL)

	require.ErrorIs(t, err, errTeapot)
	require.NotNil(t, res, "Expected the attempt's response to be handed to the caller on a policy abort")

	assert.Equal(t, "short and stout", readBody(t, res))
}

func TestClient_RetryIdempotentRequestOverride(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetryIdempotent(true),
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
	)

	res, err := client.Request().
		Method(hqgohttpmethod.POST).
		URL(server.URL).
		WithOptions(hqgohttp.WithRequestRetryIdempotent(false)).
		Context(t.Context()).Do()

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.Error(t, err)
	assert.Nil(t, res)

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, 3, retryErr.Attempts)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestClient_RetriesServerErrors(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(hqgohttpstatus.InternalServerError.Int())

			return
		}

		_, _ = w.Write([]byte("finally"))
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 4}),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, hqgohttpstatus.OK.Int(), res.StatusCode)
	assert.Equal(t, "finally", readBody(t, res))
	assert.Equal(t, int32(3), attempts.Load(), "Expected two failed attempts followed by a success")
}

func TestClient_RetryIdempotent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		method     hqgohttpmethod.Method
		idempotent bool
	}{
		{hqgohttpmethod.GET, true},
		{hqgohttpmethod.POST, false},
		{hqgohttpmethod.PUT, true},
		{hqgohttpmethod.PATCH, false},
		{hqgohttpmethod.DELETE, true},
		{hqgohttpmethod.QUERY, true},
	}

	for _, tc := range testCases {
		t.Run(tc.method.String(), func(t *testing.T) {
			t.Parallel()

			var attempts atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))

			t.Cleanup(server.Close)

			client := hqgohttp.New(
				hqgohttp.WithRetryIdempotent(true),
				hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
			)

			res, err := client.Request().Method(tc.method).URL(server.URL).Context(t.Context()).Do()

			if tc.idempotent {
				require.Error(t, err)

				var retryErr *hqgohttp.RetryError

				require.ErrorAs(t, err, &retryErr)
				assert.Equal(t, 3, retryErr.Attempts)
				assert.Nil(t, res)
				assert.Equal(t, int32(3), attempts.Load())

				return
			}

			require.NoError(t, err)
			require.NotNil(t, res)
			assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
			require.NoError(t, res.Body.Close())
			assert.Equal(t, int32(1), attempts.Load())
		})
	}
}

func TestClient_ExhaustedRetries(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)

		w.WriteHeader(hqgohttpstatus.ServiceUnavailable.Int())
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetrier(testRetrier{maxAttempts: 3}),
	)

	res, err := client.Get(server.URL)

	require.Error(t, err)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "giving up after 3 attempt(s)")
	assert.Contains(t, err.Error(), "503")
	assert.Equal(t, int32(3), attempts.Load(),
		"Expected the initial attempt plus two retries")
}

func TestClient_MalformedURLs(t *testing.T) {
	t.Parallel()

	type capture struct {
		requestURI string
		path       string
		rawPath    string
		rawQuery   string
	}

	newServer := func(t *testing.T) (server *httptest.Server, captures <-chan capture) {
		t.Helper()

		ch := make(chan capture, 1)

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ch <- capture{
				requestURI: r.RequestURI,
				path:       r.URL.Path,
				rawPath:    r.URL.RawPath,
				rawQuery:   r.URL.RawQuery,
			}

			w.WriteHeader(http.StatusNoContent)
		}))

		t.Cleanup(server.Close)

		captures = ch

		return server, captures
	}

	t.Run("invalid percent escape", func(t *testing.T) {
		t.Parallel()

		server, captures := newServer(t)

		parser := hqgohttprequest.URLParserFunc(func(raw string) (*neturl.URL, error) {
			return neturl.Parse(strings.ReplaceAll(raw, "%invalid", "%25invalid"))
		})

		client := hqgohttp.New(hqgohttp.WithURLParser(parser))

		res, err := client.Get(server.URL + "/%invalid")

		require.NoError(t, err)
		require.NotNil(t, res)
		require.NoError(t, res.Body.Close())

		got := <-captures

		assert.Equal(t, "/%25invalid", got.requestURI)
		assert.Equal(t, "/%invalid", got.path)
		assert.Empty(t, got.rawPath)
	})

	t.Run("preserved encoded path", func(t *testing.T) {
		t.Parallel()

		server, captures := newServer(t)

		client := hqgohttp.New()

		res, err := client.Get(server.URL + "/w%0d%2e/")

		require.NoError(t, err)
		require.NotNil(t, res)
		require.NoError(t, res.Body.Close())

		got := <-captures

		assert.Equal(t, "/w%0d%2e/", got.requestURI)
		assert.Equal(t, "/w\r./", got.path)
		assert.Equal(t, "/w%0d%2e/", got.rawPath)
	})

	t.Run("raw query", func(t *testing.T) {
		t.Parallel()

		server, captures := newServer(t)

		const rawQuery = "some'param=`'+OR+ORDER+BY+1--"

		client := hqgohttp.New()

		res, err := client.Get(server.URL + "/with/path?" + rawQuery)

		require.NoError(t, err)
		require.NotNil(t, res)
		require.NoError(t, res.Body.Close())

		got := <-captures

		assert.Equal(t, "/with/path?"+rawQuery, got.requestURI)
		assert.Equal(t, rawQuery, got.rawQuery)
	})
}

func TestDefaultOptionConstants(t *testing.T) {
	t.Parallel()

	assert.Zero(t, hqgohttp.DefaultTimeout)
	assert.Equal(t, 30*time.Second, hqgohttp.RecommendedTimeout)
	assert.Zero(t, hqgohttp.DefaultResponseBodyTimeout)
	assert.Equal(t, 4096, hqgohttp.DefaultResponseReadLimit)
	assert.NotEmpty(t, hqgohttp.DefaultUserAgent)
}

const protocolTestBody = "protocol test body"

func protocolTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, protocolTestBody)
	})
}

func protocolTLSTrust(server *httptest.Server) (config *tls.Config) {
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())

	config = &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}

	return config
}

func readProtocolBody(t *testing.T, body io.ReadCloser) (content string) {
	t.Helper()

	require.NotNil(t, body)

	data, err := io.ReadAll(body)

	require.NoError(t, err)
	require.NoError(t, body.Close())

	content = string(data)

	return content
}

func TestClient_ProtocolHTTP1(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(protocolTestHandler())

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithProtocol(hqgohttp.ProtocolHTTP1),
		hqgohttp.WithTLSConfig(protocolTLSTrust(server)),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	assert.Equal(t, 1, res.ProtoMajor)
	assert.Equal(t, protocolTestBody, readProtocolBody(t, res.Body))
}

func TestClient_ProtocolHTTP2(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(protocolTestHandler())
	server.EnableHTTP2 = true
	server.StartTLS()

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithProtocol(hqgohttp.ProtocolHTTP2),
		hqgohttp.WithTLSConfig(protocolTLSTrust(server)),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	assert.Equal(t, 2, res.ProtoMajor)
	assert.Equal(t, protocolTestBody, readProtocolBody(t, res.Body))
}

func TestClient_ProtocolHTTP2_h2c(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(protocolTestHandler())
	server.Config.Protocols = new(http.Protocols)
	server.Config.Protocols.SetUnencryptedHTTP2(true)
	server.Start()

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithProtocol(hqgohttp.ProtocolHTTP2),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	assert.Equal(t, 2, res.ProtoMajor)
	assert.Equal(t, protocolTestBody, readProtocolBody(t, res.Body))
}

func TestClient_ProtocolHTTP3(t *testing.T) {
	t.Parallel()

	url, tlsConfig := newProtocolH3Server(t)

	client := hqgohttp.New(
		hqgohttp.WithProtocol(hqgohttp.ProtocolHTTP3),
		withoutProxy(),
		hqgohttp.WithTLSConfig(tlsConfig),
	)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	res, err := client.Request().Context(ctx).URL(url).Do()

	require.NoError(t, err)
	assert.Equal(t, 3, res.ProtoMajor)
	assert.Equal(t, protocolTestBody, readProtocolBody(t, res.Body))
}

func TestClient_ProtocolAutoHTTP3(t *testing.T) {
	t.Parallel()

	url, tlsConfig := newProtocolH3Server(t)

	client := hqgohttp.New(
		hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3),
		withoutProxy(),
		hqgohttp.WithTLSConfig(tlsConfig),
	)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	res, err := client.Request().Context(ctx).URL(url).Do()

	require.NoError(t, err)
	assert.Equal(t, 3, res.ProtoMajor)
	assert.Equal(t, protocolTestBody, readProtocolBody(t, res.Body))
}

// newProtocolH3Server starts an HTTP/3 server on a loopback UDP socket and
// returns its URL plus a TLS configuration that trusts its certificate.
func newProtocolH3Server(t *testing.T) (url string, tlsConfig *tls.Config) {
	t.Helper()

	certServer := httptest.NewTLSServer(http.NotFoundHandler())

	t.Cleanup(certServer.Close)

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})

	require.NoError(t, err)

	server := &http3.Server{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{certServer.TLS.Certificates[0]},
		},
		Handler: protocolTestHandler(),
	}

	go func() {
		_ = server.Serve(conn)
	}()

	t.Cleanup(func() {
		_ = server.Close()
		_ = conn.Close()
	})

	url = fmt.Sprintf("https://%s/", conn.LocalAddr().String())
	tlsConfig = protocolTLSTrust(certServer)

	return url, tlsConfig
}

// withoutProxy disables environment proxying for the test client.
func withoutProxy() hqgohttp.ClientOptionFunc {
	return hqgohttp.WithProxy(func(*http.Request) (*neturl.URL, error) {
		return nil, nil
	})
}

func Test_Client_New_IgnoresNilOptions(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New(nil)
	require.NotNil(t, client)

	client.Close()
}

func TestClient_URLParser(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("parsed"))
	}))

	t.Cleanup(server.Close)

	var gotRaw string

	parser := hqgohttprequest.URLParserFunc(func(raw string) (*neturl.URL, error) {
		gotRaw = raw

		return neturl.Parse(server.URL)
	})

	client := hqgohttp.New(hqgohttp.WithURLParser(parser))

	res, err := client.Get("custom://ignored")

	require.NoError(t, err)
	assert.Equal(t, "parsed", readBody(t, res))
	assert.Equal(t, "custom://ignored", gotRaw, "Expected the custom parser to receive the raw URL")
}

func TestClient_RequestURLParserOverride(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Cleanup(server.Close)

	serverURL, err := neturl.Parse(server.URL)
	require.NoError(t, err)

	var clientCalls, requestCalls atomic.Int32

	clientParser := hqgohttprequest.URLParserFunc(func(string) (*neturl.URL, error) {
		clientCalls.Add(1)

		return serverURL, nil
	})

	requestParser := hqgohttprequest.URLParserFunc(func(string) (*neturl.URL, error) {
		requestCalls.Add(1)

		return serverURL, nil
	})

	client := hqgohttp.New(hqgohttp.WithURLParser(clientParser))

	res, err := client.Get("ignored", hqgohttp.WithRequestURLParser(requestParser))

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Zero(t, clientCalls.Load(), "Expected the per-request parser to replace the client parser")
	assert.Equal(t, int32(1), requestCalls.Load())

	res, err = client.Get("ignored", hqgohttp.WithRequestURLParser(nil))

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, int32(1), clientCalls.Load(), "Expected a nil override to keep the client parser")
	assert.Equal(t, int32(1), requestCalls.Load())
}

// ExampleClient_Get demonstrates the convenience method with a local server.
func ExampleClient_Get() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, "hello")
	}))

	defer server.Close()

	client := hqgohttp.New()

	res, err := client.Get(server.URL)
	if err != nil {
		fmt.Println(err)

		return
	}

	defer func() { _ = res.Body.Close() }()

	body, err := res.Body.String()
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(res.StatusCode)
	fmt.Println(body)
	// Output:
	// 200
	// hello
}

// ExampleClient_Request demonstrates the fluent request builder.
func ExampleClient_Request() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//nolint:gosec // Test server echoing the request is the point of the example; no untrusted rendering happens.
		_, _ = w.Write([]byte(r.Method + " " + r.URL.Query().Get("q")))
	}))

	defer server.Close()

	client := hqgohttp.New()

	res, err := client.Request().
		Method(hqgohttpmethod.GET).
		URL(server.URL).
		Param("q", "value").
		Do()
	if err != nil {
		fmt.Println(err)

		return
	}

	defer func() { _ = res.Body.Close() }()

	body, err := res.Body.String()
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(body)
	// Output:
	// GET value
}
