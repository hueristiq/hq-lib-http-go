package http_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestClientTransportExhaustion(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("connection unavailable")

	var attempts atomic.Int32

	client := mustNew(t,
		hqgohttp.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)

			return nil, transportErr
		})}),
		hqgohttp.WithRetryMaxAttempts(2),
	)

	res, err := client.Do(newRequest(t, http.MethodGet, "http://example.com/resource", http.NoBody)) //nolint:bodyclose // Exhaustion returns no response.
	require.ErrorIs(t, err, hqgohttp.ErrRetriesExhausted)
	require.ErrorIs(t, err, transportErr)
	assert.Nil(t, res)
	assert.Equal(t, int32(2), attempts.Load())

	var retryErr *hqgohttp.RetryError

	require.ErrorAs(t, err, &retryErr)
	assert.Equal(t, 2, retryErr.Attempts)
	assert.Equal(t, http.MethodGet, retryErr.Method)
	assert.Equal(t, "http://example.com/resource", retryErr.URL)
	require.Len(t, retryErr.History, 2)
	assert.Equal(t, 1, retryErr.History[0].Attempt)
	assert.Equal(t, 2, retryErr.History[1].Attempt)
}

type countingBody struct {
	reader io.Reader
	read   atomic.Int64
	closed atomic.Bool
}

func (b *countingBody) Read(p []byte) (n int, err error) {
	n, err = b.reader.Read(p)
	b.read.Add(int64(n))

	return n, err
}

func (b *countingBody) Close() (err error) {
	b.closed.Store(true)

	return nil
}

func TestClientRetriedBodyDrainIsBounded(t *testing.T) {
	t.Parallel()

	first := &countingBody{reader: strings.NewReader("0123456789")}

	var calls atomic.Int32

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Header:     make(http.Header),
				Body:       first,
				Request:    req,
			}, nil
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})
	client := mustNew(t,
		hqgohttp.WithHTTPClient(&http.Client{Transport: transport}),
		hqgohttp.WithResponseReadLimit(4),
		hqgohttp.WithRetryMaxAttempts(2),
	)

	res, err := client.Do(newRequest(t, http.MethodGet, "http://example.com", http.NoBody))
	require.NoError(t, err)
	assert.Equal(t, "ok", readBody(t, res))
	assert.LessOrEqual(t, first.read.Load(), int64(5))
	assert.True(t, first.closed.Load())
}

func TestRetryErrorContracts(t *testing.T) {
	t.Parallel()

	var nilErr *hqgohttp.RetryError

	assert.Equal(t, "http.Client.Do: giving up", nilErr.Error())
	require.NoError(t, nilErr.Unwrap())
	require.ErrorIs(t, nilErr, hqgohttp.ErrRetriesExhausted)

	cause := errors.New("cause")
	retryErr := &hqgohttp.RetryError{
		Attempts: 2,
		Method:   http.MethodGet,
		URL:      "http://example.com",
		Err:      cause,
	}

	assert.Contains(t, retryErr.Error(), "giving up after 2 attempt(s)")
	require.ErrorIs(t, retryErr, cause)
	require.ErrorIs(t, retryErr, hqgohttp.ErrRetriesExhausted)
	require.NotErrorIs(t, retryErr, hqgohttp.ErrBodyNotReplayable)
}
