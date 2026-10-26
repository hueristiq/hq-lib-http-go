package request_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("*ReusableReadCloser", func(t *testing.T) {
		t.Parallel()

		rr, err := hqgohttprequest.NewReusableReadCloser("pointer body")

		require.NoError(t, err)
		require.NotNil(t, rr)

		req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", rr)

		require.NoError(t, err)
		require.NotNil(t, req)
		require.NotNil(t, req.Body)

		expected := "pointer body"

		require.Equal(t, int64(len(expected)), req.ContentLength)

		buf := make([]byte, len(expected))

		n, err := req.Body.Read(buf)

		require.NoError(t, err)

		assert.Equal(t, len(expected), n)
		assert.Equal(t, expected, string(buf[:n]))
	})

	t.Run("func() (io.Reader, error)", func(t *testing.T) {
		t.Parallel()

		bodyFunc := func() (io.Reader, error) {
			return strings.NewReader("function body"), nil
		}

		req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", bodyFunc)

		require.NoError(t, err)
		require.NotNil(t, req)
		require.NotNil(t, req.Body)

		expected := "function body"

		require.Equal(t, int64(len(expected)), req.ContentLength)

		buf := make([]byte, len(expected))

		n, err := req.Body.Read(buf)

		require.NoError(t, err)

		assert.Equal(t, len(expected), n)
		assert.Equal(t, expected, string(buf[:n]))
	})

	t.Run("nil body", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "GET", "http://example.com", nil)

		require.NoError(t, err)
		require.NotNil(t, req)
		require.Nil(t, req.Body)
		require.Equal(t, int64(0), req.ContentLength)
	})

	t.Run("empty body uses http.NoBody", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", "")

		require.NoError(t, err)
		assert.Equal(t, http.NoBody, req.Body)
		require.NotNil(t, req.GetBody)

		body, gerr := req.GetBody()

		require.NoError(t, gerr)
		assert.Equal(t, http.NoBody, body)
		assert.Zero(t, req.ContentLength)
	})

	t.Run("nil ctx", func(t *testing.T) {
		t.Parallel()

		var nilCtx context.Context

		req, err := hqgohttprequest.New(nilCtx, "POST", "http://example.com", "nil ctx body")

		require.NoError(t, err)
		require.NotNil(t, req)
		require.NotNil(t, req.Body)
		require.Equal(t, int64(len("nil ctx body")), req.ContentLength)

		buf := make([]byte, len("nil ctx body"))

		n, err := req.Body.Read(buf)

		require.NoError(t, err)

		assert.Equal(t, "nil ctx body", string(buf[:n]))
	})

	t.Run("string body", func(t *testing.T) {
		t.Parallel()

		bodyStr := "example body"

		req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", bodyStr)

		require.NoError(t, err)
		require.NotNil(t, req)
		require.NotNil(t, req.Body)
		require.Equal(t, int64(len(bodyStr)), req.ContentLength)

		buf := make([]byte, len(bodyStr))

		n, err := req.Body.Read(buf)

		require.NoError(t, err)

		assert.Equal(t, len(bodyStr), n)
		assert.Equal(t, bodyStr, string(buf[:n]))

		buf2 := make([]byte, len(bodyStr))

		n, err = req.Body.Read(buf2)

		require.ErrorIs(t, err, io.EOF)
		assert.Zero(t, n)

		require.NotNil(t, req.GetBody)

		replayed, err := req.GetBody()

		require.NoError(t, err)
		require.NotNil(t, replayed)

		n, err = replayed.Read(buf2)

		require.NoError(t, err)

		assert.Equal(t, len(bodyStr), n)
		assert.Equal(t, bodyStr, string(buf2[:n]))
	})

	t.Run("[]byte body", func(t *testing.T) {
		t.Parallel()

		bodyBytes := []byte("byte body")

		req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", bodyBytes)

		require.NoError(t, err)
		require.NotNil(t, req)
		require.NotNil(t, req.Body)

		expected := "byte body"

		require.Equal(t, int64(len(expected)), req.ContentLength)

		buf := make([]byte, len(expected))

		n, err := req.Body.Read(buf)

		require.NoError(t, err)

		assert.Equal(t, len(expected), n)
		assert.Equal(t, expected, string(buf[:n]))
	})

	t.Run("unsupported body type", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", 123)

		require.Error(t, err)
		require.ErrorIs(t, err, hqgohttprequest.ErrUnsupportedBodyType)
		require.Nil(t, req)
	})

	t.Run("invalid method", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "BAD METHOD", "http://example.com", nil)

		require.Error(t, err)
		require.Nil(t, req)
	})

	t.Run("body function error", func(t *testing.T) {
		t.Parallel()

		bodyErr := errors.New("body function failed")
		bodyFunc := func() (io.Reader, error) {
			return nil, bodyErr
		}

		req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", bodyFunc)

		require.ErrorIs(t, err, bodyErr)
		require.Nil(t, req)
	})

	t.Run("body function reader error", func(t *testing.T) {
		t.Parallel()

		bodyFunc := func() (io.Reader, error) {
			return errorReader{err: errors.New("read failed")}, nil
		}

		req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", bodyFunc)

		require.Error(t, err)
		require.Nil(t, req)
	})

	t.Run("body reader error", func(t *testing.T) {
		t.Parallel()

		readErr := errors.New("read failed")

		req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", errorReader{err: readErr})

		require.ErrorIs(t, err, readErr)
		require.Nil(t, req)
	})

	t.Run("scheme-less URL fails without a parser", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "GET", "example.com/path", nil)

		require.Error(t, err)
		assert.Nil(t, req)
	})

	t.Run("scheme defaulting with a custom parser", func(t *testing.T) {
		t.Parallel()

		parser := hqgohttprequest.URLParserFunc(func(raw string) (*url.URL, error) {
			return url.Parse("http://" + raw)
		})

		req, err := hqgohttprequest.New(t.Context(), "GET", "example.com/path", nil, hqgohttprequest.WithURLParser(parser))

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, "http", req.URL.Scheme)
		assert.Equal(t, "example.com", req.URL.Host)
		assert.Equal(t, "/path", req.URL.Path)
	})

	t.Run("invalid URL", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "GET", "http://exa mple.com", nil)

		require.Error(t, err)
		require.Nil(t, req)
	})
}

func TestNew_ReusableBodyIsCloned(t *testing.T) {
	t.Parallel()

	source, err := hqgohttprequest.NewReusableReadCloser("hello world")

	require.NoError(t, err)

	first, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com/first", source)

	require.NoError(t, err)

	second, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com/second", source)

	require.NoError(t, err)

	firstBody, err := io.ReadAll(first.Body)

	require.NoError(t, err)
	assert.Equal(t, "hello world", string(firstBody))
	assert.Equal(t, int64(len("hello world")), first.ContentLength)

	secondBody, err := io.ReadAll(second.Body)

	require.NoError(t, err)
	assert.Equal(t, "hello world", string(secondBody))
	assert.Equal(t, int64(len("hello world")), second.ContentLength)

	sourceBody, err := io.ReadAll(source)

	require.NoError(t, err)
	assert.Equal(t, "hello world", string(sourceBody),
		"Expected the source cursor to be untouched")

	_, err = hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com/limited", source, 4)

	require.ErrorIs(t, err, hqgohttprequest.ErrBodyTooLarge)
}

func TestNew_MalformedURLs(t *testing.T) {
	t.Parallel()

	t.Run("invalid percent escape fails without a lenient parser", func(t *testing.T) {
		t.Parallel()

		const rawURL = "https://scanme.sh/%invalid"

		req, err := hqgohttprequest.New(t.Context(), "GET", rawURL, nil)

		require.Error(t, err)
		assert.Nil(t, req)
	})

	t.Run("invalid percent escape repaired by a custom parser", func(t *testing.T) {
		t.Parallel()

		const rawURL = "https://scanme.sh/%invalid"

		parser := hqgohttprequest.URLParserFunc(func(raw string) (*url.URL, error) {
			return url.Parse(strings.ReplaceAll(raw, "%invalid", "%25invalid"))
		})

		req, err := hqgohttprequest.New(t.Context(), "GET", rawURL, nil, hqgohttprequest.WithURLParser(parser))

		require.NoError(t, err)
		require.NotNil(t, req)

		assert.Equal(t, "https", req.URL.Scheme)
		assert.Equal(t, "scanme.sh", req.URL.Host)
		assert.Equal(t, "/%invalid", req.URL.Path)
		assert.Empty(t, req.URL.RawPath, "Expected net/url to encode the repaired path directly")
		assert.Equal(t, "/%25invalid", req.URL.EscapedPath())
	})

	t.Run("preserved encoded path", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "GET", "https://scanme.sh/w%0d%2e/", nil)

		require.NoError(t, err)
		require.NotNil(t, req)

		assert.Equal(t, "/w\r./", req.URL.Path)
		assert.Equal(t, "/w%0d%2e/", req.URL.RawPath)
		assert.Equal(t, "/w%0d%2e/", req.URL.EscapedPath())
	})

	t.Run("raw query is preserved", func(t *testing.T) {
		t.Parallel()

		const rawQuery = "some'param=`'+OR+ORDER+BY+1--"

		req, err := hqgohttprequest.New(t.Context(), "GET", "https://scanme.sh/with/path?"+rawQuery, nil)

		require.NoError(t, err)
		require.NotNil(t, req)

		assert.Equal(t, "https", req.URL.Scheme)
		assert.Equal(t, "scanme.sh", req.URL.Host)
		assert.Equal(t, "/with/path", req.URL.Path)
		assert.Equal(t, rawQuery, req.URL.RawQuery)
	})

	t.Run("valid escape is not double encoded", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "GET", "https://scanme.sh/a%2Fb", nil)

		require.NoError(t, err)
		require.NotNil(t, req)

		assert.Equal(t, "/a/b", req.URL.Path)
		assert.Equal(t, "/a%2Fb", req.URL.RawPath)
		assert.Equal(t, "/a%2Fb", req.URL.EscapedPath())
	})
}

func TestNew_Context(t *testing.T) {
	t.Parallel()

	type Key string

	ctx := context.WithValue(t.Context(), Key("key"), "value")

	req, err := hqgohttprequest.New(ctx, "GET", "http://example.com", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	val := req.Request.Context().Value(Key("key"))

	require.Equal(t, "value", val)
}

// errorReader is an io.Reader that fails every read.
//
// Fields:
//   - err (error): the failure returned by Read.
type errorReader struct {
	err error
}

// Read fails with the configured error.
//
// Parameters:
//   - _ ([]byte): the ignored destination buffer.
//
// Returns:
//   - n (int): always zero.
//   - err (error): the configured failure.
func (r errorReader) Read(_ []byte) (n int, err error) {
	return 0, r.err
}

func TestNewLimited(t *testing.T) {
	t.Parallel()

	t.Run("string within limit", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", "1234", 4)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, int64(4), req.ContentLength)
	})

	t.Run("string over limit", func(t *testing.T) {
		t.Parallel()

		_, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", "12345", 4)

		require.ErrorIs(t, err, hqgohttprequest.ErrBodyTooLarge)
	})

	t.Run("reader within limit", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", strings.NewReader("1234"), 4)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, int64(4), req.ContentLength)
	})

	t.Run("reader over limit", func(t *testing.T) {
		t.Parallel()

		_, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", strings.NewReader("12345"), 4)

		require.ErrorIs(t, err, hqgohttprequest.ErrBodyTooLarge)
	})

	t.Run("reusable body over limit", func(t *testing.T) {
		t.Parallel()

		body, err := hqgohttprequest.NewReusableReadCloser("12345")

		require.NoError(t, err)

		_, err = hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", body, 4)

		require.ErrorIs(t, err, hqgohttprequest.ErrBodyTooLarge)
	})

	t.Run("body function over limit", func(t *testing.T) {
		t.Parallel()

		body := func() (io.Reader, error) {
			return strings.NewReader("12345"), nil
		}

		_, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", body, 4)

		require.ErrorIs(t, err, hqgohttprequest.ErrBodyTooLarge)
	})

	t.Run("zero limit is unlimited", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", "12345", 0)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, int64(5), req.ContentLength)
	})
}

func TestNewLimited_BodyKinds(t *testing.T) {
	t.Parallel()

	t.Run("byte slice", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", []byte("1234"), 4)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, int64(4), req.ContentLength)
	})

	t.Run("byte slice over limit", func(t *testing.T) {
		t.Parallel()

		_, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", []byte("12345"), 4)

		require.ErrorIs(t, err, hqgohttprequest.ErrBodyTooLarge)
	})

	t.Run("byte slice pointer", func(t *testing.T) {
		t.Parallel()

		data := []byte("1234")

		req, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", &data, 4)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, int64(4), req.ContentLength)
	})

	t.Run("buffer", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", bytes.NewBufferString("1234"), 4)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, int64(4), req.ContentLength)
	})

	t.Run("buffer over limit", func(t *testing.T) {
		t.Parallel()

		_, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", bytes.NewBufferString("12345"), 4)

		require.ErrorIs(t, err, hqgohttprequest.ErrBodyTooLarge)
	})

	t.Run("bytes reader", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", bytes.NewReader([]byte("1234")), 4)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, int64(4), req.ContentLength)
	})

	t.Run("section reader", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com",
			io.NewSectionReader(strings.NewReader("1234"), 0, 4), 4)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, int64(4), req.ContentLength)
	})

	t.Run("read seeker", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com",
			readSeeker{Reader: strings.NewReader("1234")}, 4)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, int64(4), req.ContentLength)
	})

	t.Run("unsupported type", func(t *testing.T) {
		t.Parallel()

		_, err := hqgohttprequest.NewLimited(t.Context(), "POST", "http://example.com", 42, 0)

		require.ErrorIs(t, err, hqgohttprequest.ErrUnsupportedBodyType)
	})
}

func TestNewStream(t *testing.T) {
	t.Parallel()

	t.Run("plain reader", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewStream(t.Context(), "POST", "http://example.com", strings.NewReader("stream"), -1)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Nil(t, req.GetBody)
		assert.Equal(t, int64(-1), req.ContentLength)
		assert.Empty(t, req.Host)

		data, err := io.ReadAll(req.Body)

		require.NoError(t, err)
		assert.Equal(t, "stream", string(data))
	})

	t.Run("read closer", func(t *testing.T) {
		t.Parallel()

		body := io.NopCloser(strings.NewReader("stream"))

		req, err := hqgohttprequest.NewStream(t.Context(), "POST", "http://example.com", body, 6)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, body, req.Body)
		assert.Equal(t, int64(6), req.ContentLength)
	})

	t.Run("nil body", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.NewStream(t.Context(), "GET", "http://example.com", nil, 0)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Nil(t, req.Body)
		assert.Zero(t, req.ContentLength)
	})

	t.Run("invalid URL", func(t *testing.T) {
		t.Parallel()

		_, err := hqgohttprequest.NewStream(t.Context(), "GET", "http://[::1", nil, 0)

		require.Error(t, err)
	})
}

func TestNewReusableReadCloserLimited(t *testing.T) {
	t.Parallel()

	t.Run("nil input", func(t *testing.T) {
		t.Parallel()

		body, err := hqgohttprequest.NewReusableReadCloserLimited(nil, 4)

		require.NoError(t, err)
		require.NotNil(t, body)

		data, err := io.ReadAll(body)

		require.NoError(t, err)
		assert.Empty(t, data)
	})

	t.Run("over the limit", func(t *testing.T) {
		t.Parallel()

		_, err := hqgohttprequest.NewReusableReadCloserLimited("12345", 4)

		require.ErrorIs(t, err, hqgohttprequest.ErrBodyTooLarge)
	})

	t.Run("max int limit reads all", func(t *testing.T) {
		t.Parallel()

		body, err := hqgohttprequest.NewReusableReadCloserLimited(strings.NewReader("12345"), math.MaxInt64)

		require.NoError(t, err)
		require.NotNil(t, body)

		data, err := io.ReadAll(body)

		require.NoError(t, err)
		assert.Equal(t, "12345", string(data))
	})

	t.Run("read error", func(t *testing.T) {
		t.Parallel()

		readErr := errors.New("read failed")

		_, err := hqgohttprequest.NewReusableReadCloserLimited(errorReader{err: readErr}, 0)

		require.ErrorIs(t, err, readErr)
	})
}

type readSeeker struct {
	*strings.Reader
}

func TestRequest_Std(t *testing.T) {
	t.Parallel()

	req, err := hqgohttprequest.New(t.Context(), "GET", "http://example.com", nil)

	require.NoError(t, err)
	assert.Same(t, req.Request, req.Std())

	var nilReq *hqgohttprequest.Request

	assert.Nil(t, nilReq.Std())
}

// closeTrackingReader records whether it has been closed.
type closeTrackingReader struct {
	io.Reader
	closed bool
}

// Close marks the reader closed.
func (r *closeTrackingReader) Close() error {
	r.closed = true

	return nil
}

func TestNewReusableReadCloserLimited_ClosesReader(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		reader := &closeTrackingReader{Reader: strings.NewReader("payload")}

		_, err := hqgohttprequest.NewReusableReadCloserLimited(reader, 0)

		require.NoError(t, err)
		assert.True(t, reader.closed, "Expected the reader to be closed")
	})

	t.Run("read error", func(t *testing.T) {
		t.Parallel()

		reader := &closeTrackingReader{Reader: iotest.ErrReader(errors.New("read failed"))}

		_, err := hqgohttprequest.NewReusableReadCloserLimited(reader, 0)

		require.Error(t, err)
		assert.True(t, reader.closed, "Expected the reader to be closed")
	})

	t.Run("limit exceeded", func(t *testing.T) {
		t.Parallel()

		reader := &closeTrackingReader{Reader: strings.NewReader("0123456789")}

		_, err := hqgohttprequest.NewReusableReadCloserLimited(reader, 4)

		require.ErrorIs(t, err, hqgohttprequest.ErrBodyTooLarge)
		assert.True(t, reader.closed, "Expected the reader to be closed")
	})
}

// nilImplementingReader is a reader whose zero value is a typed nil.
type nilImplementingReader struct{}

func (*nilImplementingReader) Read([]byte) (int, error) {
	panic("Read must not be called on a typed-nil reader")
}

func TestNewReusableReadCloserLimited_TypedNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body any
	}{
		{name: "bytes reader", body: (*bytes.Reader)(nil)},
		{name: "strings reader", body: (*strings.Reader)(nil)},
		{name: "section reader", body: (*io.SectionReader)(nil)},
		{name: "reader interface", body: (*nilImplementingReader)(nil)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader, err := hqgohttprequest.NewReusableReadCloserLimited(test.body, 0)

			require.NoError(t, err)
			require.NotNil(t, reader)

			data, err := io.ReadAll(reader)

			require.NoError(t, err)
			assert.Empty(t, data)
		})
	}
}

func TestNew_NilBodyFunc(t *testing.T) {
	t.Parallel()

	var bodyFunc func() (io.Reader, error)

	req, err := hqgohttprequest.New(t.Context(), "POST", "https://example.com", bodyFunc)

	require.NoError(t, err)
	assert.Nil(t, req.Body)
}

func TestNew_NilParserResult(t *testing.T) {
	t.Parallel()

	parser := hqgohttprequest.URLParserFunc(func(string) (*url.URL, error) {
		return nil, nil //nolint:nilnil // The test deliberately exercises a nil parser result.
	})

	req, err := hqgohttprequest.New(t.Context(), "GET", "https://example.com", nil, hqgohttprequest.WithURLParser(parser))

	require.Error(t, err)
	assert.Nil(t, req)
}
