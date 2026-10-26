package http_test

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgohttpheader "github.com/hueristiq/hq-lib-http-go/header"
	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
	hqgohttpmime "github.com/hueristiq/hq-lib-http-go/mime"
	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
	hqgohttpstatus "github.com/hueristiq/hq-lib-http-go/status"
)

// requestMirror captures what a request assembled by the builder looks like
// on the wire.
//
// Fields:
//   - method (string): the HTTP method.
//   - url (string): the full request URL.
//   - host (string): the Host header seen by the server.
//   - headers (http.Header): the request headers.
//   - body (string): the request body.
type requestMirror struct {
	method  string
	url     string
	host    string
	headers http.Header
	body    string
}

// newMirrorServer starts an httptest server echoing the request's method and
// URL in the response body, recording the full request into mirror.
func newMirrorServer(t *testing.T, mirror *requestMirror) (serverURL string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			body = nil
		}

		mirror.method = r.Method
		mirror.url = r.URL.String()
		mirror.host = r.Host
		mirror.headers = r.Header.Clone()
		mirror.body = string(body)

		//nolint:gosec // Echoing the method and URL in a test server is not an XSS vector.
		_, _ = w.Write([]byte(r.Method + " " + r.URL.String()))
	}))

	t.Cleanup(server.Close)

	serverURL = server.URL

	return
}

func TestRequestBuilder_EndToEnd(t *testing.T) {
	t.Parallel()

	var mirror requestMirror

	serverURL := newMirrorServer(t, &mirror)

	client := hqgohttp.New()

	res, err := client.Request().
		Method(hqgohttpmethod.POST).
		URL(serverURL+"/users").
		Param("page", "1").
		Param("filter", "active").
		SetHeader(hqgohttpheader.ContentType.String(), hqgohttpmime.JSON.String()).
		AddHeader(hqgohttpheader.Accept.String(), hqgohttpmime.JSON.String()).
		Body(`{"name":"ada"}`).
		Context(t.Context()).Do()

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, hqgohttpstatus.OK.Int(), res.StatusCode)
	assert.Equal(t, "POST /users?filter=active&page=1", readBody(t, res))

	assert.Equal(t, "POST", mirror.method)
	assert.Equal(t, hqgohttpmime.JSON.String(), mirror.headers.Get(hqgohttpheader.ContentType.String()))
	assert.JSONEq(t, `{"name":"ada"}`, mirror.body)

	parsed, err := url.Parse(serverURL + mirror.url)

	require.NoError(t, err)

	query := parsed.Query()

	assert.Equal(t, "1", query.Get("page"))
	assert.Equal(t, "active", query.Get("filter"))
}

func TestRequestBuilder_ParamPreservesRawQuery(t *testing.T) {
	t.Parallel()

	var mirror requestMirror

	serverURL := newMirrorServer(t, &mirror)

	client := hqgohttp.New()

	const rawQuery = "b=2&sig=abc%2Fdef&empty="

	res, err := client.Request().
		URL(serverURL+"/search?"+rawQuery).
		Param("page", "2").
		Context(t.Context()).Do()

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, "/search?"+rawQuery+"&page=2", mirror.url)
}

func TestRequestBuilder_EmptyBodyContentLength(t *testing.T) {
	t.Parallel()

	newRecorder := func(t *testing.T) (serverURL string, contentLength *atomic.Int64, chunked *atomic.Bool) {
		t.Helper()

		contentLength = new(atomic.Int64)
		chunked = new(atomic.Bool)

		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			contentLength.Store(r.ContentLength)
			chunked.Store(len(r.TransferEncoding) > 0)
		}))

		t.Cleanup(server.Close)

		return server.URL, contentLength, chunked
	}

	t.Run("empty string", func(t *testing.T) {
		t.Parallel()

		serverURL, contentLength, chunked := newRecorder(t)

		res, err := hqgohttp.New().Post(serverURL, "")

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Zero(t, contentLength.Load())
		assert.False(t, chunked.Load())
	})

	t.Run("empty bytes", func(t *testing.T) {
		t.Parallel()

		serverURL, contentLength, chunked := newRecorder(t)

		res, err := hqgohttp.New().Post(serverURL, []byte{})

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Zero(t, contentLength.Load())
		assert.False(t, chunked.Load())
	})

	t.Run("empty form", func(t *testing.T) {
		t.Parallel()

		serverURL, contentLength, chunked := newRecorder(t)

		res, err := hqgohttp.New().Request().
			Method(hqgohttpmethod.POST).
			URL(serverURL).
			Form(url.Values{}).
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Zero(t, contentLength.Load())
		assert.False(t, chunked.Load())
	})
}

func TestRequestBuilder_ReaderBodyReusable(t *testing.T) {
	t.Parallel()

	var bodies []string

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
	}))

	t.Cleanup(server.Close)

	builder := hqgohttp.New().Request().
		Method(hqgohttpmethod.POST).
		URL(server.URL).
		Body(strings.NewReader("payload"))

	for range 2 {
		res, err := builder.Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
	}

	assert.Equal(t, []string{"payload", "payload"}, bodies)
}

func TestRequestBuilder_ReaderBodyError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")

	res, err := hqgohttp.New().Request().
		Method(hqgohttpmethod.POST).
		URL("http://example.com").
		Body(iotest.ErrReader(readErr)).
		Do()

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.ErrorIs(t, err, readErr)
}

func TestRequestBuilder_Headers(t *testing.T) {
	t.Parallel()

	t.Run("set replaces, add appends", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		res, err := client.Request().
			URL(serverURL).
			SetHeader("X-Set", "first").
			SetHeader("X-Set", "second").
			AddHeader("X-Add", "one").
			AddHeader("X-Add", "two").
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, []string{"second"}, mirror.headers.Values("X-Set"))
		assert.Equal(t, []string{"one", "two"}, mirror.headers.Values("X-Add"))
	})

	t.Run("add then set replaces earlier adds", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		res, err := client.Request().
			URL(serverURL).
			AddHeader("X-Mixed", "add").
			SetHeader("X-Mixed", "set").
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, []string{"set"}, mirror.headers.Values("X-Mixed"))
	})

	t.Run("set then add appends", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		res, err := client.Request().
			URL(serverURL).
			SetHeader("X-Mixed", "set").
			AddHeader("X-Mixed", "add").
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, []string{"set", "add"}, mirror.headers.Values("X-Mixed"))
	})

	t.Run("last host value supplied wins", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		res, err := client.Request().
			URL(serverURL).
			AddHeader("Host", "first.example").
			SetHeader("Host", "second.example").
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, "second.example", mirror.host)
	})

	t.Run("request header overrides client default", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New(hqgohttp.WithHeader("X-Default", "client-value"))

		res, err := client.Request().
			URL(serverURL).
			SetHeader("X-Default", "request-value").
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, []string{"request-value"}, mirror.headers.Values("X-Default"))
	})

	t.Run("client default header is applied", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New(hqgohttp.WithHeader("X-Default", "client-value"))

		res, err := client.Request().
			URL(serverURL).
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, []string{"client-value"}, mirror.headers.Values("X-Default"))
	})

	t.Run("host header reaches the server", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		res, err := client.Request().
			URL(serverURL).
			SetHeader("Host", "override.example").
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, "override.example", mirror.host)
	})

	t.Run("client default host is applied", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New(hqgohttp.WithHeader("Host", "default.example"))

		res, err := client.Request().
			URL(serverURL).
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, "default.example", mirror.host)
	})

	t.Run("request host overrides client default", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New(hqgohttp.WithHeader("Host", "default.example"))

		res, err := client.Request().
			URL(serverURL).
			SetHeader("Host", "request.example").
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		assert.Equal(t, "request.example", mirror.host)
	})
}

func TestRequestBuilder_DefaultMethod(t *testing.T) {
	t.Parallel()

	var mirror requestMirror

	serverURL := newMirrorServer(t, &mirror)

	client := hqgohttp.New()

	res, err := client.Request().
		URL(serverURL).
		Context(t.Context()).Do()

	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, hqgohttpmethod.GET.String(), mirror.method)
}

func TestRequestBuilder_Invalid(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New()

	t.Run("no URL configured", func(t *testing.T) {
		t.Parallel()

		res, err := client.Request().
			Method(hqgohttpmethod.GET).
			Context(t.Context()).Do()

		require.Error(t, err)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		assert.Nil(t, res)
		assert.Contains(t, err.Error(), "no URL configured")
	})

	t.Run("relative URL without a host", func(t *testing.T) {
		t.Parallel()

		res, err := client.Request().
			URL("/relative").
			Context(t.Context()).Do()

		require.Error(t, err)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		assert.Nil(t, res)
	})
}

func TestRequestBuilder_WithOptions(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)

		w.WriteHeader(hqgohttpstatus.ServiceUnavailable.Int())
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetryMax(5),
		hqgohttp.WithRetryBackoff(noWaitBackoff),
	)

	res, err := client.Request().
		URL(server.URL).
		WithOptions(hqgohttp.WithRequestRetryMax(1)).
		Context(t.Context()).Do()

	require.Error(t, err)

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	assert.Nil(t, res)
	assert.Equal(t, int32(2), attempts.Load(),
		"Expected the builder-registered request options to override the client configuration")
}

func TestRequestBuilder_NilContext(t *testing.T) {
	t.Parallel()

	var mirror requestMirror

	serverURL := newMirrorServer(t, &mirror)

	client := hqgohttp.New()

	var nilCtx context.Context

	res, err := client.Request().
		URL(serverURL + "/nil-ctx").
		Context(nilCtx).
		Do()

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "GET /nil-ctx", readBody(t, res))
}

func TestRequestBuilder_Context(t *testing.T) {
	t.Parallel()

	t.Run("canceled builder context", func(t *testing.T) {
		t.Parallel()

		serverURL := newOKServer(t)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		client := hqgohttp.New()

		res, err := client.Request().URL(serverURL).Context(ctx).Do()

		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, res)
	})

	t.Run("client default context", func(t *testing.T) {
		t.Parallel()

		type contextKey struct{}

		ctx := context.WithValue(t.Context(), contextKey{}, "client-default")

		serverURL := newOKServer(t)

		client := hqgohttp.New(hqgohttp.WithContext(ctx))

		res, err := client.Get(serverURL)

		require.NoError(t, err)
		assert.Equal(t, "ok", readBody(t, res))
		assert.Equal(t, "client-default", res.Request.Context().Value(contextKey{}))
	})

	t.Run("builder context overrides the client default", func(t *testing.T) {
		t.Parallel()

		serverURL := newOKServer(t)

		defaultCtx, cancel := context.WithCancel(t.Context())
		cancel()

		client := hqgohttp.New(hqgohttp.WithContext(defaultCtx))

		res, err := client.Request().Context(t.Context()).URL(serverURL).Do()

		require.NoError(t, err)
		assert.Equal(t, "ok", readBody(t, res))
	})

	t.Run("request context overrides the client default", func(t *testing.T) {
		t.Parallel()

		serverURL := newOKServer(t)

		defaultCtx, cancel := context.WithCancel(t.Context())
		cancel()

		client := hqgohttp.New(hqgohttp.WithContext(defaultCtx))

		raw, err := http.NewRequestWithContext(t.Context(), http.MethodGet, serverURL, http.NoBody)

		require.NoError(t, err)

		res, err := client.Do(&hqgohttprequest.Request{Request: raw})

		require.NoError(t, err)
		assert.Equal(t, "ok", readBody(t, res))
	})

	t.Run("nil client default", func(t *testing.T) {
		t.Parallel()

		serverURL := newOKServer(t)

		client := hqgohttp.New(hqgohttp.WithContext(nil)) //nolint:staticcheck // WithContext(nil) deliberately exercises the no-default path.

		res, err := client.Get(serverURL)

		require.NoError(t, err)
		assert.Equal(t, "ok", readBody(t, res))
	})

	t.Run("last client default wins", func(t *testing.T) {
		t.Parallel()

		serverURL := newOKServer(t)

		canceled, cancel := context.WithCancel(t.Context())
		cancel()

		client := hqgohttp.New(
			hqgohttp.WithContext(canceled),
			hqgohttp.WithContext(t.Context()),
		)

		res, err := client.Get(serverURL)

		require.NoError(t, err)
		assert.Equal(t, "ok", readBody(t, res))
	})
}

type jsonInputPayload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type xmlInputPayload struct {
	XMLName xml.Name `xml:"user"`
	Name    string   `xml:"name"`
	Age     int      `xml:"age"`
}

func TestRequestBuilder_JSON(t *testing.T) {
	t.Parallel()

	t.Run("encodes the body and content type", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)

				return
			}

			//nolint:gosec // G705: the test server echoes its own request data.
			_, _ = io.WriteString(w, r.Header.Get("Content-Type")+"\n"+string(body))
		}))

		t.Cleanup(server.Close)

		res, err := hqgohttp.New().Request().
			Method(hqgohttpmethod.POST).
			URL(server.URL).
			JSON(jsonInputPayload{Name: "alice", Age: 30}).
			Context(t.Context()).Do()

		require.NoError(t, err)

		assert.Equal(t, "application/json\n{\"name\":\"alice\",\"age\":30}", readBody(t, res))
	})

	t.Run("explicit content type wins", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//nolint:gosec // G705: the test server echoes its own request data.
			_, _ = io.WriteString(w, r.Header.Get("Content-Type"))
		}))

		t.Cleanup(server.Close)

		res, err := hqgohttp.New().Request().
			Method(hqgohttpmethod.POST).
			URL(server.URL).
			SetHeader("Content-Type", "application/vnd.api+json").
			JSON(jsonInputPayload{Name: "alice"}).
			Context(t.Context()).Do()

		require.NoError(t, err)
		assert.Equal(t, "application/vnd.api+json", readBody(t, res))
	})

	t.Run("client default content type wins", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//nolint:gosec // G705: the test server echoes its own request data.
			_, _ = io.WriteString(w, r.Header.Get("Content-Type"))
		}))

		t.Cleanup(server.Close)

		client := hqgohttp.New(hqgohttp.WithHeader("Content-Type", "text/plain"))

		res, err := client.Request().
			Method(hqgohttpmethod.POST).
			URL(server.URL).
			JSON(jsonInputPayload{Name: "alice"}).
			Context(t.Context()).Do()

		require.NoError(t, err)
		assert.Equal(t, "text/plain", readBody(t, res))
	})

	t.Run("marshal error surfaces from Do", func(t *testing.T) {
		t.Parallel()

		var attempts atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			w.WriteHeader(http.StatusOK)
		}))

		t.Cleanup(server.Close)

		res, err := hqgohttp.New().Request().
			Method(hqgohttpmethod.POST).
			URL(server.URL).
			JSON(func() {}).
			Context(t.Context()).Do()

		require.Error(t, err)
		require.ErrorContains(t, err, "encoding request body")
		assert.Nil(t, res)
		assert.Zero(t, attempts.Load())
	})
}

func TestRequestBuilder_XML(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		//nolint:gosec // G705: the test server echoes its own request data.
		_, _ = io.WriteString(w, r.Header.Get("Content-Type")+"\n"+string(body))
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Request().
		Method(hqgohttpmethod.POST).
		URL(server.URL).
		XML(xmlInputPayload{Name: "alice", Age: 30}).
		Context(t.Context()).Do()

	require.NoError(t, err)

	assert.Equal(t, "application/xml\n<user><name>alice</name><age>30</age></user>", readBody(t, res))
}

func TestRequestBuilder_Form(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		//nolint:gosec // G705: the test server echoes its own request data.
		_, _ = io.WriteString(w, r.Header.Get("Content-Type")+"\n"+r.PostForm.Encode())
	}))

	t.Cleanup(server.Close)

	values := url.Values{
		"name": {"alice"},
		"tag":  {"one", "two"},
	}

	res, err := hqgohttp.New().Request().
		Method(hqgohttpmethod.POST).
		URL(server.URL).
		Form(values).
		Context(t.Context()).Do()

	require.NoError(t, err)

	assert.Equal(t, "application/x-www-form-urlencoded\nname=alice&tag=one&tag=two", readBody(t, res))
}

func TestRequestBuilder_Multipart(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		if err := r.ParseMultipartForm(1 << 20); err != nil { //nolint:gosec // G120: the body is bounded with MaxBytesReader.
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		file, header, err := r.FormFile("upload")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		if err = file.Close(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		//nolint:gosec // G705: the test server echoes its own request data.
		_, _ = io.WriteString(w, r.FormValue("description")+"|"+header.Filename+"|"+string(data))
	}))

	t.Cleanup(server.Close)

	form := hqgohttp.NewMultipartForm().
		Field("description", "a note").
		File("upload", "note.txt", strings.NewReader("file-content"))

	res, err := hqgohttp.New().Request().
		Method(hqgohttpmethod.POST).
		URL(server.URL).
		Multipart(form).
		Context(t.Context()).Do()

	require.NoError(t, err)

	assert.Equal(t, "a note|note.txt|file-content", readBody(t, res))
}

func TestRequestBuilder_MultipartRetry(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		if err := r.ParseMultipartForm(1 << 20); err != nil { //nolint:gosec // G120: the body is bounded with MaxBytesReader.
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		file, header, err := r.FormFile("upload")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		if err = file.Close(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		if count == 1 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		//nolint:gosec // G705: the test server echoes its own request data.
		_, _ = io.WriteString(w, r.FormValue("description")+"|"+header.Filename+"|"+string(data))
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetryMax(1),
		hqgohttp.WithRetryBackoff(noWaitBackoff),
		hqgohttp.WithRetryIdempotent(false),
	)

	form := hqgohttp.NewMultipartForm().
		Field("description", "retried").
		File("upload", "note.txt", strings.NewReader("file-content"))

	res, err := client.Request().
		Method(hqgohttpmethod.POST).
		URL(server.URL).
		Multipart(form).
		Context(t.Context()).Do()

	require.NoError(t, err)

	assert.Equal(t, "retried|note.txt|file-content", readBody(t, res))
	assert.Equal(t, int32(2), attempts.Load())
}

func TestRequestBuilder_MultipartNilReader(t *testing.T) {
	t.Parallel()

	form := hqgohttp.NewMultipartForm().File("upload", "note.txt", nil)

	res, err := hqgohttp.New().Request().
		Method(hqgohttpmethod.POST).
		URL("https://example.com").
		Multipart(form).
		Context(t.Context()).Do()

	require.Error(t, err)
	require.ErrorContains(t, err, "nil reader")
	assert.Nil(t, res)
}

// countingReader counts the bytes read from the wrapped reader.
type countingReader struct {
	io.Reader
	read int64
}

func (r *countingReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.read += int64(n)

	return n, err
}

func TestRequestBuilder_MultipartBodyLimit(t *testing.T) {
	t.Parallel()

	const (
		limit = 256
		size  = 1 << 20
	)

	reader := &countingReader{Reader: strings.NewReader(strings.Repeat("x", size))}

	form := hqgohttp.NewMultipartForm().File("upload", "big.bin", reader)

	res, err := hqgohttp.New(hqgohttp.WithMaxRequestBodySize(limit)).Request().
		Method(hqgohttpmethod.POST).
		URL("https://example.com").
		Multipart(form).
		Context(t.Context()).Do()

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.ErrorIs(t, err, hqgohttp.ErrRequestBodyTooLarge)
	assert.Less(t, reader.read, int64(size),
		"Expected the multipart encoder to stop before reading the whole file")
}

func TestRequestBuilder_LastBodySetterWins(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		//nolint:gosec // G705: the test server echoes its own request data.
		_, _ = io.WriteString(w, r.Header.Get("Content-Type")+"\n"+string(body))
	}))

	t.Cleanup(server.Close)

	t.Run("JSON after Body", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New().Request().
			Method(hqgohttpmethod.POST).
			URL(server.URL).
			Body("raw").
			JSON(jsonInputPayload{Name: "alice"}).
			Context(t.Context()).Do()

		require.NoError(t, err)
		assert.Equal(t, "application/json\n{\"name\":\"alice\",\"age\":0}", readBody(t, res))
	})

	t.Run("Body after JSON", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New().Request().
			Method(hqgohttpmethod.POST).
			URL(server.URL).
			JSON(jsonInputPayload{Name: "alice"}).
			Body("raw").
			Context(t.Context()).Do()

		require.NoError(t, err)
		assert.Equal(t, "\nraw", readBody(t, res))
	})
}

func TestRequest_Std(t *testing.T) {
	t.Parallel()

	req, err := hqgohttprequest.New(context.Background(), http.MethodGet, "https://example.com", nil)

	require.NoError(t, err)
	assert.Same(t, req.Request, req.Std())

	var nilRequest *hqgohttprequest.Request

	assert.Nil(t, nilRequest.Std())
}

func TestRequestBuilder_URLResolution_HostPort(t *testing.T) {
	t.Parallel()

	t.Run("IPv4 host and port", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		res, err := client.Request().
			URL(strings.TrimPrefix(serverURL, "http://") + "/direct").
			Context(t.Context()).Do()

		require.NoError(t, err)

		assert.Equal(t, "GET /direct", readBody(t, res))
	})

	t.Run("localhost host and port", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		_, port, err := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
		require.NoError(t, err)

		client := hqgohttp.New()

		res, err := client.Request().
			URL("localhost:" + port + "/direct").
			Context(t.Context()).Do()

		require.NoError(t, err)

		assert.Equal(t, "GET /direct", readBody(t, res))
	})
}

func TestRequestBuilder_URLResolution(t *testing.T) {
	t.Parallel()

	t.Run("scheme defaulting", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		host := serverURL[len("http://"):]

		res, err := client.Request().
			URL(host + "/schemeless").
			Context(t.Context()).Do()

		require.NoError(t, err)

		assert.Equal(t, "GET /schemeless", readBody(t, res))
	})

	t.Run("relative URL query is preserved", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		res, err := client.Request().
			URL(serverURL + "/search?q=go&x=1").
			Context(t.Context()).Do()

		require.NoError(t, err)

		assert.Equal(t, "GET /search?q=go&x=1", readBody(t, res))
	})

	t.Run("query containing an absolute URL is preserved", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		res, err := client.Request().
			URL(serverURL + "/jump?next=https://example.com").
			Context(t.Context()).Do()

		require.NoError(t, err)

		assert.Equal(t, "GET /jump?next=https://example.com", readBody(t, res))
	})

	t.Run("invalid reference is repaired", func(t *testing.T) {
		t.Parallel()

		var mirror requestMirror

		serverURL := newMirrorServer(t, &mirror)

		client := hqgohttp.New()

		res, err := client.Request().
			URL(serverURL + "/%zz").
			Context(t.Context()).Do()

		require.NoError(t, err)
		require.NotNil(t, res)

		assert.Equal(t, "GET /%25zz", readBody(t, res))
	})
}

func TestRequestBuilder_PathOnlyURLRejected(t *testing.T) {
	t.Parallel()

	client := hqgohttp.New()

	res, err := client.Request().
		URL("/users").
		Do()

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no host")
}

func TestRequestBuilder_MaxRequestBodySize(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = w.Write(body)
	}))

	t.Cleanup(server.Close)

	t.Run("raw body over the limit", func(t *testing.T) {
		t.Parallel()

		before := attempts.Load()

		client := hqgohttp.New(
			hqgohttp.WithMaxRequestBodySize(4),
			hqgohttp.WithRetryMax(0),
		)

		res, err := client.Post(server.URL, "0123456789")

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		require.ErrorIs(t, err, hqgohttp.ErrRequestBodyTooLarge)
		assert.Equal(t, before, attempts.Load(), "Expected the oversized request not to be sent")
	})

	t.Run("raw body within the limit", func(t *testing.T) {
		t.Parallel()

		client := hqgohttp.New(
			hqgohttp.WithMaxRequestBodySize(4),
			hqgohttp.WithRetryMax(0),
		)

		res, err := client.Post(server.URL, "0123")

		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, "0123", readBody(t, res))
		require.NoError(t, res.Body.Close())
	})

	t.Run("encoded JSON over the limit", func(t *testing.T) {
		t.Parallel()

		before := attempts.Load()

		client := hqgohttp.New(
			hqgohttp.WithMaxRequestBodySize(4),
			hqgohttp.WithRetryMax(0),
		)

		res, err := client.Request().
			Method(hqgohttpmethod.POST).
			URL(server.URL).
			JSON(map[string]string{"value": "0123456789"}).
			Do()

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		require.ErrorIs(t, err, hqgohttp.ErrRequestBodyTooLarge)
		assert.Equal(t, before, attempts.Load(), "Expected the oversized request not to be sent")
	})

	t.Run("pre-built request over the limit", func(t *testing.T) {
		t.Parallel()

		before := attempts.Load()

		req, err := hqgohttprequest.New(t.Context(), http.MethodPost, server.URL, "0123456789")

		require.NoError(t, err)

		client := hqgohttp.New(
			hqgohttp.WithMaxRequestBodySize(4),
			hqgohttp.WithRetryMax(0),
		)

		res, err := client.Do(req)

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		require.ErrorIs(t, err, hqgohttp.ErrRequestBodyTooLarge)
		assert.Equal(t, before, attempts.Load(), "Expected the oversized request not to be sent")
	})

	t.Run("stream with a known length over the limit", func(t *testing.T) {
		t.Parallel()

		before := attempts.Load()

		client := hqgohttp.New(
			hqgohttp.WithMaxRequestBodySize(4),
			hqgohttp.WithRetryMax(0),
		)

		res, err := client.Request().
			Method(hqgohttpmethod.POST).
			URL(server.URL).
			BodyStream(strings.NewReader("0123456789"), 10).
			Do()

		if res != nil {
			require.NoError(t, res.Body.Close())
		}

		require.ErrorIs(t, err, hqgohttp.ErrRequestBodyTooLarge)
		assert.Equal(t, before, attempts.Load(), "Expected the oversized request not to be sent")
	})
}

func TestMultipartForm_Stream(t *testing.T) {
	t.Parallel()

	t.Run("streams fields and files", func(t *testing.T) {
		t.Parallel()

		form := hqgohttp.NewMultipartForm().
			Field("name", "ada").
			File("avatar", "avatar.txt", strings.NewReader("hello"))

		body, contentType, err := form.Stream()

		require.NoError(t, err)
		require.NotNil(t, body)
		t.Cleanup(func() {
			_ = body.Close()
		})

		mediaType, params, err := mime.ParseMediaType(contentType)

		require.NoError(t, err)
		assert.Equal(t, "multipart/form-data", mediaType)
		require.NotEmpty(t, params["boundary"])

		parsed, err := multipart.NewReader(body, params["boundary"]).ReadForm(1 << 20)

		require.NoError(t, err)
		require.Len(t, parsed.Value["name"], 1)
		assert.Equal(t, "ada", parsed.Value["name"][0])

		files := parsed.File["avatar"]

		require.Len(t, files, 1)

		file, err := files[0].Open()

		require.NoError(t, err)

		data, err := io.ReadAll(file)

		require.NoError(t, err)
		assert.Equal(t, "hello", string(data))
		require.NoError(t, file.Close())
		require.NoError(t, parsed.RemoveAll())
	})

	t.Run("rejects a nil file reader", func(t *testing.T) {
		t.Parallel()

		form := hqgohttp.NewMultipartForm().
			File("avatar", "avatar.txt", nil)

		body, _, err := form.Stream()

		require.Error(t, err)
		assert.Nil(t, body)
	})

	t.Run("works through BodyStream", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//nolint:gosec // G120: the test server bounds the form at 1 MiB.
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)

				return
			}

			//nolint:gosec // G705: the test server echoes its own request data.
			_, _ = io.WriteString(w, r.FormValue("name"))
		}))

		t.Cleanup(server.Close)

		form := hqgohttp.NewMultipartForm().Field("name", "ada")
		body, contentType, err := form.Stream()

		require.NoError(t, err)

		client := hqgohttp.New(hqgohttp.WithRetryMax(0))

		res, err := client.Request().
			Method(hqgohttpmethod.POST).
			URL(server.URL).
			BodyStream(body, -1).
			SetHeader(hqgohttpheader.ContentType.String(), contentType).
			Do()

		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, "ada", readBody(t, res))
		require.NoError(t, res.Body.Close())
	})
}

func TestRequestBuilder_BodyStreamIsNotReplayed(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)

		_, _ = io.Copy(io.Discard, r.Body)

		w.WriteHeader(http.StatusInternalServerError)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(
		hqgohttp.WithRetryMax(2),
		hqgohttp.WithRetryBackoff(noWaitBackoff),
	)

	res, err := client.Request().
		Method(hqgohttpmethod.PUT).
		URL(server.URL).
		BodyStream(strings.NewReader("payload"), int64(len("payload"))).
		Do()

	if res != nil {
		require.NoError(t, res.Body.Close())
	}

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	require.ErrorIs(t, err, hqgohttp.ErrBodyNotReplayable)
	require.NotErrorIs(t, err, hqgohttp.ErrRetriesExhausted)
	assert.Equal(t, 1, retryErr.Attempts)
	assert.Contains(t, err.Error(), hqgohttp.ErrBodyNotReplayable.Error())
	assert.Equal(t, int32(1), attempts.Load(),
		"Expected a streamed body to stop before another attempt")
}

func TestRequestBuilder_BodyStream(t *testing.T) {
	t.Parallel()

	newServer := func(t *testing.T, gotLength *atomic.Int64) (serverURL string) {
		t.Helper()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotLength.Store(r.ContentLength)

			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)

				return
			}

			_, _ = w.Write(body)
		}))

		t.Cleanup(server.Close)

		return server.URL
	}

	t.Run("sends the stream and its length", func(t *testing.T) {
		t.Parallel()

		payload := "streamed payload"

		var gotLength atomic.Int64

		serverURL := newServer(t, &gotLength)

		client := hqgohttp.New(hqgohttp.WithRetryMax(0))

		res, err := client.Request().
			Method(hqgohttpmethod.POST).
			URL(serverURL).
			BodyStream(strings.NewReader(payload), int64(len(payload))).
			Do()

		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, payload, readBody(t, res))
		assert.Equal(t, int64(len(payload)), gotLength.Load())
		require.NoError(t, res.Body.Close())
	})

	t.Run("stream overrides a prior body", func(t *testing.T) {
		t.Parallel()

		var gotLength atomic.Int64

		serverURL := newServer(t, &gotLength)

		client := hqgohttp.New(hqgohttp.WithRetryMax(0))

		res, err := client.Request().
			Method(hqgohttpmethod.POST).
			URL(serverURL).
			Body("old body").
			BodyStream(strings.NewReader("new"), 3).
			Do()

		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, "new", readBody(t, res))
		require.NoError(t, res.Body.Close())
	})

	t.Run("a later body setter replaces the stream", func(t *testing.T) {
		t.Parallel()

		var gotLength atomic.Int64

		serverURL := newServer(t, &gotLength)

		client := hqgohttp.New(hqgohttp.WithRetryMax(0))

		res, err := client.Request().
			Method(hqgohttpmethod.POST).
			URL(serverURL).
			BodyStream(strings.NewReader("stream"), 6).
			Body("replacement").
			Do()

		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, "replacement", readBody(t, res))
		require.NoError(t, res.Body.Close())
	})
}

func TestStripSensitiveHeadersOnRedirect(t *testing.T) {
	t.Parallel()

	newRequest := func(t *testing.T, rawURL string) (req *http.Request) {
		t.Helper()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, http.NoBody)

		require.NoError(t, err)

		req.Header.Set("X-Api-Key", "secret")
		req.Header.Set("X-Token", "token")

		return req
	}

	t.Run("same host keeps the headers", func(t *testing.T) {
		t.Parallel()

		policy := hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key")

		req := newRequest(t, "https://example.com/next")

		require.NoError(t, policy(req, []*http.Request{newRequest(t, "https://example.com/start")}))
		assert.Equal(t, "secret", req.Header.Get("X-Api-Key"))
	})

	t.Run("cross host strips the headers", func(t *testing.T) {
		t.Parallel()

		policy := hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key")

		req := newRequest(t, "https://other.example/next")

		require.NoError(t, policy(req, []*http.Request{newRequest(t, "https://example.com/start")}))
		assert.Empty(t, req.Header.Get("X-Api-Key"))
		assert.Equal(t, "token", req.Header.Get("X-Token"),
			"Expected only the listed headers to be stripped")
	})

	t.Run("https downgrade strips the headers", func(t *testing.T) {
		t.Parallel()

		policy := hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key")

		req := newRequest(t, "http://example.com/next")

		require.NoError(t, policy(req, []*http.Request{newRequest(t, "https://example.com/start")}))
		assert.Empty(t, req.Header.Get("X-Api-Key"))
	})

	t.Run("multiple keys", func(t *testing.T) {
		t.Parallel()

		policy := hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key", "x-token")

		req := newRequest(t, "https://other.example/next")

		require.NoError(t, policy(req, []*http.Request{newRequest(t, "https://example.com/start")}))
		assert.Empty(t, req.Header.Get("X-Api-Key"))
		assert.Empty(t, req.Header.Get("X-Token"))
	})

	t.Run("empty via keeps the headers", func(t *testing.T) {
		t.Parallel()

		policy := hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key")

		req := newRequest(t, "https://example.com/next")

		require.NoError(t, policy(req, nil))
		assert.Equal(t, "secret", req.Header.Get("X-Api-Key"))
	})

	t.Run("nil URLs do not panic", func(t *testing.T) {
		t.Parallel()

		policy := hqgohttp.StripSensitiveHeadersOnRedirect("X-Api-Key")

		require.NoError(t, policy(&http.Request{Header: make(http.Header)}, []*http.Request{{}}))
	})
}

// newOKServer starts a test server that answers every request with "ok".
func newOKServer(t *testing.T) (serverURL string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))

	t.Cleanup(server.Close)

	return server.URL
}

func Test_Client_Do_IgnoresNilRequestOptions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := hqgohttp.New()
	t.Cleanup(client.Close)

	req, err := hqgohttprequest.New(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	res, err := client.Do(req, nil)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
}
