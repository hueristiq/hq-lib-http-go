package http

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseBody_ReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")

	ctx, cancel := context.WithCancelCause(t.Context())

	body := &responseBody{
		ReadCloser: reviewErrorBody{err: readErr},
		cancel:     cancel,
		cause: func() error {
			return context.Cause(ctx)
		},
	}

	_, err := body.Read(make([]byte, 1))

	require.ErrorIs(t, err, readErr)
	require.ErrorIs(t, context.Cause(ctx), context.Canceled)
}

func TestSnapshotBody_MaxInt64Limit(t *testing.T) {
	t.Parallel()

	data, truncated, err := snapshotBody(io.NopCloser(strings.NewReader("payload")), math.MaxInt64)

	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))
	assert.False(t, truncated)
}

func TestSnapshotBody_ReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")

	data, truncated, err := snapshotBody(io.NopCloser(reviewErrorBody{err: readErr}), 4)

	require.ErrorIs(t, err, readErr)
	assert.False(t, truncated)
	assert.Empty(t, data)
}

// eofCountingBody reports how many Read calls reached it, including the final
// EOF observation.
type eofCountingBody struct {
	data  []byte
	calls int
}

func (b *eofCountingBody) Read(p []byte) (n int, err error) {
	b.calls++

	if len(b.data) == 0 {
		return 0, io.EOF
	}

	n = copy(p, b.data)
	b.data = b.data[n:]

	return n, nil
}

func (b *eofCountingBody) Close() error { return nil }

func TestDrainBody_ObservesEOFAtExactLimit(t *testing.T) {
	t.Parallel()

	body := &eofCountingBody{data: []byte("12345678")}

	drainBody(body, 8)

	assert.Equal(t, 2, body.calls,
		"Expected the drain to read past the limit and observe EOF")
}
