package http_test

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

type closeCountingBody struct {
	io.Reader

	closes atomic.Int32
}

func (b *closeCountingBody) Close() error {
	b.closes.Add(1)

	return nil
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (failingReadCloser) Close() error {
	return nil
}

func TestResponse_JSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"name":"alice","age":30}`)
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.IsSuccess())

	var payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	require.NoError(t, res.Body.JSON(&payload))

	assert.Equal(t, "alice", payload.Name)
	assert.Equal(t, 30, payload.Age)
}

func TestResponse_XML(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<user><name>alice</name><age>30</age></user>")
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	var payload struct {
		Name string `xml:"name"`
		Age  int    `xml:"age"`
	}

	require.NoError(t, res.Body.XML(&payload))

	assert.Equal(t, "alice", payload.Name)
	assert.Equal(t, 30, payload.Age)
}

func TestResponse_BytesCache(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "cached")
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	first, err := res.Body.Bytes()

	require.NoError(t, err)
	assert.Equal(t, "cached", string(first))

	second, err := res.Body.Bytes()

	require.NoError(t, err)
	assert.Equal(t, "cached", string(second))

	text, err := res.Body.String()

	require.NoError(t, err)
	assert.Equal(t, "cached", text)

	replayed, err := io.ReadAll(res.Body)

	require.NoError(t, err)
	assert.Equal(t, "cached", string(replayed))

	require.NoError(t, res.Body.Close())
	require.NoError(t, res.Body.Close())
}

func TestResponse_StatusClassification(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))

		w.WriteHeader(code)
	}))

	t.Cleanup(server.Close)

	client := hqgohttp.New(hqgohttp.WithRetryMax(0))

	tests := []struct {
		code                int
		success             bool
		redirection         bool
		clientError         bool
		serverError         bool
		errorClassification bool
	}{
		{code: http.StatusOK, success: true},
		{code: http.StatusMovedPermanently, redirection: true},
		{code: http.StatusNotFound, clientError: true, errorClassification: true},
		{code: http.StatusNotImplemented, serverError: true, errorClassification: true},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.code), func(t *testing.T) {
			t.Parallel()

			res, err := client.Get(server.URL + "/" + strconv.Itoa(tt.code))

			require.NoError(t, err)
			require.NotNil(t, res)

			assert.Equal(t, tt.success, res.IsSuccess())
			assert.Equal(t, tt.redirection, res.IsRedirection())
			assert.Equal(t, tt.clientError, res.IsClientError())
			assert.Equal(t, tt.serverError, res.IsServerError())
			assert.Equal(t, tt.errorClassification, res.IsError())
		})
	}

	t.Run("informational", func(t *testing.T) {
		t.Parallel()

		res := &hqgohttp.Response{Response: &http.Response{StatusCode: http.StatusContinue}}

		assert.True(t, res.IsInformational())
		assert.False(t, res.IsSuccess())
		assert.False(t, res.IsError())
	})
}

func TestResponse_Non2xxIsNotAnError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "missing")
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New(hqgohttp.WithRetryMax(0)).Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.IsClientError())
	assert.True(t, res.IsError())
	assert.False(t, res.IsSuccess())
	assert.Equal(t, "missing", readBody(t, res))
}

func TestResponse_ReadError(t *testing.T) {
	t.Parallel()

	transport := reviewRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       failingReadCloser{},
		}, nil
	})

	res, err := hqgohttp.New(hqgohttp.WithTransport(transport)).Get("https://example.com")

	require.NoError(t, err)
	require.NotNil(t, res)

	_, err = res.Body.Bytes()

	require.ErrorContains(t, err, "read failed")
}

func TestResponse_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	body := &closeCountingBody{Reader: strings.NewReader("payload")}

	transport := reviewRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
		}, nil
	})

	res, err := hqgohttp.New(hqgohttp.WithTransport(transport)).Get("https://example.com")

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NoError(t, res.Body.Close())
	require.NoError(t, res.Body.Close())

	assert.Equal(t, int32(1), body.closes.Load())
}

func TestBody_StreamCached(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "cached")
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	data, err := res.Body.Bytes()

	require.NoError(t, err)
	assert.Equal(t, "cached", string(data))

	stream := res.Body.Stream()

	require.NotNil(t, stream)

	cached, err := io.ReadAll(stream)

	require.NoError(t, err)
	assert.Equal(t, "cached", string(cached))
	require.NoError(t, stream.Close())
}

func TestBody_Stream(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "streamed")
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	stream := res.Body.Stream()

	require.NotNil(t, stream)

	data, err := io.ReadAll(stream)

	require.NoError(t, err)
	assert.Equal(t, "streamed", string(data))
	require.NoError(t, stream.Close())
	require.NoError(t, res.Body.Close())
}

func TestBody_MaxResponseBodySize(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "0123456789")
	}))

	t.Cleanup(server.Close)

	t.Run("over the limit returns the prefix and an error", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New(hqgohttp.WithMaxResponseBodySize(5)).Get(server.URL)

		require.NoError(t, err)
		require.NotNil(t, res)

		data, err := res.Body.Bytes()

		require.ErrorIs(t, err, hqgohttp.ErrResponseBodyTooLarge)
		assert.Equal(t, "01234", string(data))
		require.NoError(t, res.Body.Close())
	})

	t.Run("exact limit succeeds", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New(hqgohttp.WithMaxResponseBodySize(10)).Get(server.URL)

		require.NoError(t, err)
		require.NotNil(t, res)

		data, err := res.Body.Bytes()

		require.NoError(t, err)
		assert.Equal(t, "0123456789", string(data))
		require.NoError(t, res.Body.Close())
	})

	t.Run("JSON wraps the limit error", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New(hqgohttp.WithMaxResponseBodySize(5)).Get(server.URL)

		require.NoError(t, err)
		require.NotNil(t, res)

		var value any

		err = res.Body.JSON(&value)

		require.ErrorIs(t, err, hqgohttp.ErrResponseBodyTooLarge)
		require.NoError(t, res.Body.Close())
	})

	t.Run("request override removes the client cap", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New(hqgohttp.WithMaxResponseBodySize(5)).
			Get(server.URL, hqgohttp.WithRequestMaxResponseBodySize(0))

		require.NoError(t, err)
		require.NotNil(t, res)

		data, err := res.Body.Bytes()

		require.NoError(t, err)
		assert.Equal(t, "0123456789", string(data))
		require.NoError(t, res.Body.Close())
	})

	t.Run("request override adds a cap", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New().
			Get(server.URL, hqgohttp.WithRequestMaxResponseBodySize(5))

		require.NoError(t, err)
		require.NotNil(t, res)

		data, err := res.Body.Bytes()

		require.ErrorIs(t, err, hqgohttp.ErrResponseBodyTooLarge)
		assert.Equal(t, "01234", string(data))
		require.NoError(t, res.Body.Close())
	})

	t.Run("math.MaxInt64 is unlimited", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New(hqgohttp.WithMaxResponseBodySize(math.MaxInt64)).Get(server.URL)

		require.NoError(t, err)
		require.NotNil(t, res)

		data, err := res.Body.Bytes()

		require.NoError(t, err)
		assert.Equal(t, "0123456789", string(data))
		require.NoError(t, res.Body.Close())
	})
}

func TestBody_UTF8(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=iso-8859-1")
		_, _ = w.Write([]byte{'c', 'a', 'f', 0xE9})
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	text, err := res.Body.UTF8()

	require.NoError(t, err)
	assert.Equal(t, "café", text)
}

func TestBody_UTF8Empty(t *testing.T) {
	t.Parallel()

	t.Run("empty 200", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
		}))
		t.Cleanup(server.Close)

		res, err := hqgohttp.New().Get(server.URL)

		require.NoError(t, err)

		text, err := res.Body.UTF8()

		require.NoError(t, err)
		assert.Empty(t, text)
	})

	t.Run("204 no content", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(server.Close)

		res, err := hqgohttp.New().Get(server.URL)

		require.NoError(t, err)

		text, err := res.Body.UTF8()

		require.NoError(t, err)
		assert.Empty(t, text)
	})

	t.Run("HEAD", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
		}))
		t.Cleanup(server.Close)

		res, err := hqgohttp.New().Head(server.URL)

		require.NoError(t, err)

		text, err := res.Body.UTF8()

		require.NoError(t, err)
		assert.Empty(t, text)
	})
}

func TestBody_ReadAfterClose(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello")
	}))
	t.Cleanup(server.Close)

	t.Run("Read", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New().Get(server.URL)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		_, err = res.Body.Read(make([]byte, 1))

		require.ErrorContains(t, err, "read on closed response body")
		require.NotErrorIs(t, err, context.Canceled)
	})

	t.Run("Bytes", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New().Get(server.URL)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		_, err = res.Body.Bytes()

		require.ErrorContains(t, err, "read on closed response body")
		require.NotErrorIs(t, err, context.Canceled)
	})

	t.Run("String", func(t *testing.T) {
		t.Parallel()

		res, err := hqgohttp.New().Get(server.URL)

		require.NoError(t, err)
		require.NoError(t, res.Body.Close())

		_, err = res.Body.String()

		require.ErrorContains(t, err, "read on closed response body")
	})
}

func TestBody_XMLCharset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><note>caf`))
		_, _ = w.Write([]byte{0xE9})
		_, _ = w.Write([]byte(`</note>`))
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)

	var note struct {
		Text string `xml:",chardata"`
	}

	err = res.Body.XML(&note)

	require.NoError(t, err)
	assert.Equal(t, "café", note.Text)
}

func TestBody_SSE(t *testing.T) {
	t.Parallel()

	t.Run("parses events", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w,
				": comment\n"+
					"event: greeting\n"+
					"id: 7\n"+
					"retry: 1500\n"+
					"data: hello\n"+
					"data: world\n"+
					"\n"+
					"data: second\n"+
					"\n",
			)
		}))

		t.Cleanup(server.Close)

		res, err := hqgohttp.New().Get(server.URL)

		require.NoError(t, err)
		require.NotNil(t, res)

		var events []hqgohttp.SSEEvent

		err = res.Body.SSE(func(event *hqgohttp.SSEEvent) bool {
			events = append(events, *event)

			return true
		})

		require.NoError(t, err)
		require.Len(t, events, 2)
		assert.Equal(t, "greeting", events[0].Event)
		assert.Equal(t, "hello\nworld", events[0].Data)
		assert.Equal(t, "7", events[0].ID)
		assert.Equal(t, 1500*time.Millisecond, events[0].Retry)
		assert.Empty(t, events[1].Event)
		assert.Equal(t, "second", events[1].Data)
		assert.Equal(t, "7", events[1].ID)
		assert.Equal(t, 1500*time.Millisecond, events[1].Retry)
	})

	t.Run("stops when the callback returns false", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "data: one\n\ndata: two\n\ndata: three\n\n")
		}))

		t.Cleanup(server.Close)

		res, err := hqgohttp.New().Get(server.URL)

		require.NoError(t, err)
		require.NotNil(t, res)

		var calls int

		err = res.Body.SSE(func(*hqgohttp.SSEEvent) bool {
			calls++

			return false
		})

		require.NoError(t, err)
		assert.Equal(t, 1, calls)
	})

	t.Run("rejects a nil handler", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "data: x\n\n")
		}))

		t.Cleanup(server.Close)

		res, err := hqgohttp.New().Get(server.URL)

		require.NoError(t, err)
		require.NotNil(t, res)

		err = res.Body.SSE(nil)

		require.Error(t, err)
	})
}

func TestBody_Read(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "read")
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	data, err := io.ReadAll(res.Body)

	require.NoError(t, err)
	assert.Equal(t, "read", string(data))
	require.NoError(t, res.Body.Close())
}

func TestResponse_Std(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.New().Get(server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Body)
	assert.Same(t, res.Response, res.Std()) //nolint:bodyclose // readBody closes the underlying body.
	assert.NotNil(t, res.Std().Body)        //nolint:bodyclose // readBody closes the underlying body.
	assert.Equal(t, "ok", readBody(t, res))
}
