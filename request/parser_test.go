package request_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
)

func TestURLParserFunc(t *testing.T) {
	t.Parallel()

	var _ hqgohttprequest.URLParser = hqgohttprequest.URLParserFunc(nil)

	want := &url.URL{Scheme: "https", Host: "example.com"}
	errParse := errors.New("parse failed")

	var gotRaw string

	parser := hqgohttprequest.URLParserFunc(func(raw string) (*url.URL, error) {
		gotRaw = raw

		return want, errParse
	})

	parsed, err := parser.Parse("raw")

	require.ErrorIs(t, err, errParse)
	assert.Same(t, want, parsed)
	assert.Equal(t, "raw", gotRaw, "Expected the adapter to receive the raw URL")
}

func TestURLParserFunc_Nil(t *testing.T) {
	t.Parallel()

	var parser hqgohttprequest.URLParserFunc

	parsed, err := parser.Parse("https://example.com")

	require.Error(t, err)
	assert.Nil(t, parsed)
	assert.Contains(t, err.Error(), "nil parser")
}

func TestNew_WithURLParser(t *testing.T) {
	t.Parallel()

	want := &url.URL{Scheme: "https", Host: "custom.example", Path: "/p"}

	var gotRaw string

	parser := hqgohttprequest.URLParserFunc(func(raw string) (*url.URL, error) {
		gotRaw = raw

		return want, nil
	})

	req, err := hqgohttprequest.New(t.Context(), "GET", "ignored", nil, hqgohttprequest.WithURLParser(parser))

	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "ignored", gotRaw, "Expected the custom parser to receive the raw URL")
	assert.Equal(t, "custom.example", req.URL.Host)
	assert.Equal(t, "/p", req.URL.Path)
}

func TestNew_DefaultURLParserIsStrict(t *testing.T) {
	t.Parallel()

	t.Run("absolute URL", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "GET", "https://example.com/path", nil)

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, "example.com", req.URL.Host)
	})

	t.Run("scheme-less URL fails", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "GET", "example.com/path", nil)

		require.Error(t, err)
		assert.Nil(t, req)
	})

	t.Run("invalid escape fails", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "GET", "https://example.com/%invalid", nil)

		require.Error(t, err)
		assert.Nil(t, req)
	})

	t.Run("nil parser keeps the default", func(t *testing.T) {
		t.Parallel()

		req, err := hqgohttprequest.New(t.Context(), "GET", "https://example.com/path", nil, hqgohttprequest.WithURLParser(nil))

		require.NoError(t, err)
		require.NotNil(t, req)
		assert.Equal(t, "example.com", req.URL.Host)
	})
}
