package http_test

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

// transportFunc adapts a function to an [http.RoundTripper].
type transportFunc func(req *http.Request) (res *http.Response, err error)

// RoundTrip executes the function.
//
// Parameters:
//   - req (*http.Request): the request being round-tripped.
//
// Returns:
//   - res (*http.Response): the response.
//   - err (error): the transport error.
func (f transportFunc) RoundTrip(req *http.Request) (res *http.Response, err error) {
	res, err = f(req)

	return
}

func TestClient_WithTransport(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	transport := transportFunc(func(req *http.Request) (res *http.Response, err error) {
		calls.Add(1)

		res = &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("custom transport")),
			Request:    req,
		}

		return
	})

	client := hqgohttp.New(hqgohttp.WithTransport(transport),
		hqgohttp.WithRetryMax(0),
	)

	res, err := client.Get("http://example.com/")

	require.NoError(t, err)
	assert.Equal(t, "custom transport", readBody(t, res))
	assert.Equal(t, int32(1), calls.Load())
}

func TestClient_WithTransportWrapper(t *testing.T) {
	t.Parallel()

	var wrapped atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "wrapped")
	}))

	t.Cleanup(server.Close)

	wrapper := func(base http.RoundTripper) http.RoundTripper {
		return transportFunc(func(req *http.Request) (res *http.Response, err error) {
			wrapped.Add(1)

			return base.RoundTrip(req)
		})
	}

	client := hqgohttp.New(hqgohttp.WithTransportWrapper(wrapper),
		hqgohttp.WithRetryMax(0),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	assert.Equal(t, "wrapped", readBody(t, res))
	assert.Equal(t, int32(1), wrapped.Load())
}

func TestClient_WithTransportWrapperPreservesHTTP2(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "h2")
	}))

	server.EnableHTTP2 = true
	server.StartTLS()

	t.Cleanup(server.Close)

	wrapper := func(base http.RoundTripper) http.RoundTripper {
		return base
	}

	client := hqgohttp.New(hqgohttp.WithProtocol(hqgohttp.ProtocolHTTP2),
		hqgohttp.WithTLSConfig(protocolTLSTrust(server)),
		hqgohttp.WithTransportWrapper(wrapper),
		hqgohttp.WithRetryMax(0),
	)

	res, err := client.Get(server.URL)

	require.NoError(t, err)
	assert.Equal(t, 2, res.ProtoMajor)
	assert.Equal(t, "h2", readBody(t, res))
}

func TestClient_WithCheckRedirect(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)

			return
		}

		_, _ = io.WriteString(w, "final")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithCheckRedirect(func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}),
		hqgohttp.WithRetryMax(0),
	)

	res, err := client.Get(server.URL + "/start")

	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, res.StatusCode)
	assert.Equal(t, "/final", res.Header.Get("Location"))
	require.NoError(t, res.Body.Close())
}

func TestClient_WithCookieJar(t *testing.T) {
	t.Parallel()

	jar, err := cookiejar.New(nil)

	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/set" {
			http.SetCookie(w, &http.Cookie{
				Name:     "session",
				Value:    "abc",
				Path:     "/",
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			w.WriteHeader(http.StatusNoContent)

			return
		}

		if _, err := r.Cookie("session"); err != nil {
			http.Error(w, "missing cookie", http.StatusBadRequest)

			return
		}

		_, _ = io.WriteString(w, "present")
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithCookieJar(jar),
		hqgohttp.WithTLSConfig(protocolTLSTrust(server)),
		hqgohttp.WithRetryMax(0),
	)

	res, err := client.Get(server.URL + "/set")

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	res, err = client.Get(server.URL + "/check")

	require.NoError(t, err)
	assert.Equal(t, "present", readBody(t, res))
}
