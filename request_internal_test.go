package http

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
)

func TestRequestOptionFuncs(t *testing.T) {
	t.Parallel()

	basePolicy := func(_ context.Context, _ *http.Request, _ *http.Response, _ error, _ int) (retry bool, rerr error) {
		return false, nil
	}

	otherPolicy := func(_ context.Context, _ *http.Request, _ *http.Response, _ error, _ int) (retry bool, rerr error) {
		return true, nil
	}

	baseLimiter := testRateLimiter{}

	baseRetrier := internalTestRetrier{maxAttempts: 1}
	otherRetrier := internalTestRetrier{maxAttempts: 2}

	opts := &RequestOptions{
		Retrier:           baseRetrier,
		RetryPolicy:       basePolicy,
		RateLimiter:       baseLimiter,
		ResponseReadLimit: 9,
	}

	WithRequestRetrier(nil)(opts)

	assert.Nil(t, opts.Retrier, "Expected a nil override to disable retries")

	WithRequestRetrier(otherRetrier)(opts)

	assert.Equal(t, otherRetrier, opts.Retrier)

	WithRequestRetryPolicy(nil)(opts)

	assert.NotNil(t, opts.RetryPolicy, "Expected a nil override to be ignored")

	WithRequestRetryPolicy(otherPolicy)(opts)

	retry, err := opts.RetryPolicy(t.Context(), nil, nil, nil, 1)

	require.NoError(t, err)
	assert.True(t, retry)

	WithRequestRateLimiter(nil)(opts)

	assert.NotNil(t, opts.RateLimiter, "Expected a nil override to be ignored")

	WithRequestRateLimiter(testRateLimiter{})(opts)

	assert.NotNil(t, opts.RateLimiter)

	WithRequestOnRetry(nil)(opts)

	assert.Nil(t, opts.OnRetry, "Expected a nil override to keep the client hook")

	WithRequestOnRetry(func(_ RetryEvent) {})(opts)

	assert.NotNil(t, opts.OnRetry)

	WithRequestOnRequest(nil)(opts)

	assert.Nil(t, opts.OnRequest, "Expected a nil override to be ignored")

	WithRequestOnRequest(func(_ RequestEvent) {})(opts)

	assert.NotNil(t, opts.OnRequest)

	WithRequestOnResponse(nil)(opts)

	assert.Nil(t, opts.OnResponse, "Expected a nil override to be ignored")

	WithRequestOnResponse(func(_ ResponseEvent) {})(opts)

	assert.NotNil(t, opts.OnResponse)

	WithRequestOnRetryExhaustion(nil)(opts)

	assert.Nil(t, opts.RetryExhaustionHandler, "Expected a nil override to be ignored")

	WithRequestOnRetryExhaustion(func(_ *http.Response, _ error, _ int) (*http.Response, error) {
		return nil, nil //nolint:nilnil // The handler deliberately declines the response.
	})(opts)

	assert.NotNil(t, opts.RetryExhaustionHandler)

	WithRequestRetryIdempotent(true)(opts)

	assert.True(t, opts.IdempotentRetries)

	WithRequestAttemptTimeout(0)(opts)

	assert.Zero(t, opts.AttemptTimeout)

	WithRequestAttemptTimeout(time.Second)(opts)

	assert.Equal(t, time.Second, opts.AttemptTimeout)

	WithRequestRetryErrorBodyLimit(0)(opts)

	assert.Zero(t, opts.RetryErrorBodyLimit)

	WithRequestRetryErrorBodyLimit(16)(opts)

	assert.Equal(t, int64(16), opts.RetryErrorBodyLimit)
}

func TestRequestBuilder_Build_AddedHeaderAppendsToClientDefault(t *testing.T) {
	t.Parallel()

	client := New(WithHeader("X-Default", "client"))

	req, err := client.Request().
		URL("http://example.com/").
		AddHeader("x-default", "request").
		build(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []string{"client", "request"}, req.Header.Values("X-Default"),
		"Expected AddHeader to append to the client default")
}

func TestRequestBuilder_Build_MissingURL(t *testing.T) {
	t.Parallel()

	_, err := New().Request().build(t.Context())

	require.ErrorIs(t, err, ErrNoURL)
}

func TestApplyDefaultHeaders(t *testing.T) {
	t.Parallel()

	headers := []HeaderField{
		{Key: "X-Default", Value: "default"},
		{Key: "Host", Value: "virtual.example"},
	}

	req := &http.Request{}
	applyDefaultHeaders(req, headers)

	assert.Equal(t, "default", req.Header.Get("X-Default"))
	assert.Equal(t, "virtual.example", req.Host)

	requestHeader := http.Header{"X-Default": []string{"request"}}
	req = &http.Request{Header: requestHeader}
	applyDefaultHeaders(req, headers)

	assert.Equal(t, "request", req.Header.Get("X-Default"))
	assert.Equal(t, "virtual.example", req.Host)

	lowercaseKey := "x-default"
	lowercaseHeader := http.Header{lowercaseKey: []string{"request"}}
	req = &http.Request{Header: lowercaseHeader}
	applyDefaultHeaders(req, headers)

	assert.Equal(t, []string{"request"}, req.Header[lowercaseKey])

	applyDefaultHeaders(req, nil)
}

func TestRequestBuilder_Build_RequestError(t *testing.T) {
	t.Parallel()

	client := New()

	_, err := client.Request().Method(hqgohttpmethod.Method("BAD METHOD")).URL("http://example.com/").build(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "building request")
}
