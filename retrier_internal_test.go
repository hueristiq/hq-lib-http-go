package http

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryAfterWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   time.Duration
		wantOK bool
	}{
		{name: "missing", wantOK: false},
		{name: "seconds", header: "2", want: 2 * time.Second, wantOK: true},
		{name: "zero", header: "0", want: 0, wantOK: true},
		{name: "negative", header: "-1", wantOK: false},
		{name: "invalid", header: "tomorrow", wantOK: false},
		{name: "past date", header: time.Unix(1, 0).UTC().Format(http.TimeFormat), want: 0, wantOK: true},
		{name: "overflow", header: "9223372036854775807", want: 2562047*time.Hour + 47*time.Minute + 16*time.Second, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := &http.Response{Header: make(http.Header)}
			if tt.header != "" {
				res.Header.Set("Retry-After", tt.header)
			}

			got, ok := retryAfterWait(res)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRewindRequestBody(t *testing.T) {
	t.Parallel()

	require.NoError(t, rewindRequestBody(&http.Request{}))
	require.NoError(t, rewindRequestBody(&http.Request{Body: http.NoBody}))
	require.ErrorIs(t, rewindRequestBody(&http.Request{Body: io.NopCloser(strings.NewReader("body"))}), ErrBodyNotReplayable)

	getBodyErr := errors.New("get body failed")
	req := &http.Request{
		Body: io.NopCloser(strings.NewReader("body")),
		GetBody: func() (io.ReadCloser, error) {
			return nil, getBodyErr
		},
	}
	require.ErrorIs(t, rewindRequestBody(req), ErrBodyNotReplayable)
	require.ErrorIs(t, rewindRequestBody(req), getBodyErr)

	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("fresh")), nil
	}
	require.NoError(t, rewindRequestBody(req))

	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, "fresh", string(body))
}
