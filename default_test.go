package http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
)

func TestDefaultHTTPPooledTransport(t *testing.T) {
	t.Parallel()

	transport := hqgohttp.DefaultHTTPPooledTransport()

	require.NotNil(t, transport)
	assert.False(t, transport.DisableKeepAlives)
	assert.True(t, transport.ForceAttemptHTTP2)
	assert.NotNil(t, transport.Proxy)
	assert.NotNil(t, transport.DialContext)
}

func TestDefaultHTTPTransport(t *testing.T) {
	t.Parallel()

	transport := hqgohttp.DefaultHTTPTransport()

	require.NotNil(t, transport)
	assert.True(t, transport.DisableKeepAlives)
	assert.Equal(t, -1, transport.MaxIdleConnsPerHost)
}

func TestDefaultHTTPClients(t *testing.T) {
	t.Parallel()

	t.Run("pooled", func(t *testing.T) {
		t.Parallel()

		client := hqgohttp.DefaultHTTPPooledClient()

		require.NotNil(t, client)

		transport, ok := client.Transport.(*http.Transport)

		require.True(t, ok, "Expected the pooled client to run on an *http.Transport")
		assert.False(t, transport.DisableKeepAlives)
	})

	t.Run("non-pooled", func(t *testing.T) {
		t.Parallel()

		client := hqgohttp.DefaultHTTPClient()

		require.NotNil(t, client)

		transport, ok := client.Transport.(*http.Transport)

		require.True(t, ok, "Expected the client to run on an *http.Transport")
		assert.True(t, transport.DisableKeepAlives)
	})
}

func TestPackageLevelShortcuts_Methods(t *testing.T) {
	t.Parallel()

	const payload = "shortcut payload"

	tests := []struct {
		name       string
		wantMethod string
		wantBody   string
		call       func(ctx context.Context, rawURL string) (res *hqgohttp.Response, err error)
	}{
		{
			name:       "Head",
			wantMethod: http.MethodHead,
			call: func(ctx context.Context, rawURL string) (res *hqgohttp.Response, err error) {
				return hqgohttp.Head(ctx, rawURL)
			},
		},
		{
			name:       "Post",
			wantMethod: http.MethodPost,
			wantBody:   payload,
			call: func(ctx context.Context, rawURL string) (res *hqgohttp.Response, err error) {
				return hqgohttp.Post(ctx, rawURL, payload)
			},
		},
		{
			name:       "Query",
			wantMethod: hqgohttpmethod.QUERY.String(),
			wantBody:   payload,
			call: func(ctx context.Context, rawURL string) (res *hqgohttp.Response, err error) {
				return hqgohttp.Query(ctx, rawURL, payload)
			},
		},
		{
			name:       "Put",
			wantMethod: http.MethodPut,
			wantBody:   payload,
			call: func(ctx context.Context, rawURL string) (res *hqgohttp.Response, err error) {
				return hqgohttp.Put(ctx, rawURL, payload)
			},
		},
		{
			name:       "Patch",
			wantMethod: http.MethodPatch,
			wantBody:   payload,
			call: func(ctx context.Context, rawURL string) (res *hqgohttp.Response, err error) {
				return hqgohttp.Patch(ctx, rawURL, payload)
			},
		},
		{
			name:       "Delete",
			wantMethod: http.MethodDelete,
			call: func(ctx context.Context, rawURL string) (res *hqgohttp.Response, err error) {
				return hqgohttp.Delete(ctx, rawURL)
			},
		},
		{
			name:       "Options",
			wantMethod: http.MethodOptions,
			call: func(ctx context.Context, rawURL string) (res *hqgohttp.Response, err error) {
				return hqgohttp.Options(ctx, rawURL)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			type request struct {
				method string
				body   string
			}

			requests := make(chan request, 1)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)

				requests <- request{method: r.Method, body: string(body)}

				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)

			res, err := tt.call(t.Context(), server.URL)

			require.NoError(t, err)
			require.NotNil(t, res)
			require.NoError(t, res.Body.Close())
			assert.Equal(t, http.StatusNoContent, res.StatusCode)

			got := <-requests

			assert.Equal(t, tt.wantMethod, got.method)
			assert.Equal(t, tt.wantBody, got.body)
		})
	}
}

func TestPackageLevelShortcuts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("package level"))
	}))

	t.Cleanup(server.Close)

	res, err := hqgohttp.Get(t.Context(), server.URL)

	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "package level", readBody(t, res))
}
