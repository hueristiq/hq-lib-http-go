package request

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReusableReadCloser_NilReceiver(t *testing.T) {
	t.Parallel()

	var closer *ReusableReadCloser

	closer.Reset()

	n, err := closer.Read(make([]byte, 1))

	assert.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)

	clone := closer.clone()

	require.NotNil(t, clone)
	assert.Empty(t, clone.data)

	data, err := io.ReadAll(clone)

	require.NoError(t, err)
	assert.Empty(t, data)
}

func TestReusableReadCloser_Clone(t *testing.T) {
	t.Parallel()

	closer, err := NewReusableReadCloser(strings.NewReader("payload"))

	require.NoError(t, err)

	clone := closer.clone()

	require.NotNil(t, clone)
	assert.NotSame(t, closer, clone)

	data, err := io.ReadAll(clone)

	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))

	// The clone has an independent cursor.
	data, err = io.ReadAll(closer)

	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))
}
