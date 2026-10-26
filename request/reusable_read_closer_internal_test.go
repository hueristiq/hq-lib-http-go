package request

import (
	"errors"
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

func TestReusableReadCloser_ZeroValueReset(t *testing.T) {
	t.Parallel()

	var closer ReusableReadCloser

	closer.Reset()

	data, err := io.ReadAll(&closer)

	require.NoError(t, err)
	assert.Empty(t, data)
}

// valueReader implements io.Reader as a value type, so its reflect kind is not
// nilable.
type valueReader struct{}

func (valueReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

func TestIsNilReader(t *testing.T) {
	t.Parallel()

	assert.True(t, isNilReader(nil))

	var pointer *ReusableReadCloser

	assert.True(t, isNilReader(pointer))
	assert.False(t, isNilReader(strings.NewReader("")))
	assert.False(t, isNilReader(valueReader{}))
}

func TestGetReusableBodyReadCloser_NilFunc(t *testing.T) {
	t.Parallel()

	reader, err := getReusableBodyReadCloser((func() (io.Reader, error))(nil), 0)

	require.NoError(t, err)
	assert.Nil(t, reader)
}

func TestGetReusableBodyReadCloser_FuncError(t *testing.T) {
	t.Parallel()

	errBody := errors.New("body failed")

	_, err := getReusableBodyReadCloser(func() (io.Reader, error) {
		return nil, errBody
	}, 0)

	require.ErrorIs(t, err, errBody)
}

func TestGetReusableBodyReadCloser_TypedNil(t *testing.T) {
	t.Parallel()

	reader, err := getReusableBodyReadCloser((*ReusableReadCloser)(nil), 0)

	require.NoError(t, err)
	assert.Nil(t, reader)
}

func TestReusableReadCloser_ZeroValueRead(t *testing.T) {
	t.Parallel()

	var closer ReusableReadCloser

	n, err := closer.Read(make([]byte, 1))

	assert.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
}

func TestNewReusableReadCloserLimited_ByteSlicePointerLimit(t *testing.T) {
	t.Parallel()

	body := []byte("0123456789")

	_, err := NewReusableReadCloserLimited(&body, 4)

	require.ErrorIs(t, err, ErrBodyTooLarge)
}
