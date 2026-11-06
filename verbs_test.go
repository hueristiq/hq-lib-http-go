package http_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestClient_ConvenienceMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//nolint:gosec // Echoing the method and path in a test server is not an XSS vector.
		_, _ = fmt.Fprintf(w, "%s %s", r.Method, r.URL.Path)
	}))

	t.Cleanup(server.Close)

	client := mustNew(t)

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

	client := mustNew(t)

	res, err := client.Query(server.URL, payload)

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "ok", readBody(t, res))
	assert.Equal(t, "QUERY", method.Load())
	assert.Equal(t, payload, body.Load())
	assert.Equal(t, int64(len(payload)), length.Load())
}
