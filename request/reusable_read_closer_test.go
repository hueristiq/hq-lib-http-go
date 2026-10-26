package request_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
)

func TestNewReusableReadCloser(t *testing.T) {
	t.Parallel()

	const inputString = "hello world"

	inputByteSlice := []byte(inputString)

	testCases := []struct {
		name        string
		input       interface{}
		expectedOut string
		expectedErr bool
	}{
		{"Nil Input", nil, "", false},
		{"Byte Slice", inputByteSlice, inputString, false},
		{"Pointer to Byte Slice", &inputByteSlice, inputString, false},
		{"String", inputString, inputString, false},
		{"*bytes.Buffer", bytes.NewBufferString(inputString), inputString, false},
		{"*bytes.Reader", bytes.NewReader(inputByteSlice), inputString, false},
		{"*strings.Reader", strings.NewReader(inputString), inputString, false},
		{"*io.SectionReader", io.NewSectionReader(bytes.NewReader(inputByteSlice), 0, int64(len(inputString))), inputString, false},
		{"io.ReadSeeker", bytes.NewReader(inputByteSlice), inputString, false},
		{"io.Reader", strings.NewReader(inputString), inputString, false},
		{"custom io.ReadSeeker", customReadSeeker{Reader: bytes.NewReader(inputByteSlice)}, inputString, false},
		{"custom io.Reader", customReader{Reader: strings.NewReader(inputString)}, inputString, false},
		{"failing reader", errorReader{err: errors.New("read failed")}, "", true},
		{"Invalid Input", 123, "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader, err := hqgohttprequest.NewReusableReadCloser(tc.input)

			if tc.expectedErr {
				require.Error(t, err, "Expected an error but got none")

				return
			}

			require.NoError(t, err, "Unexpected error occurred")

			buf := make([]byte, len(tc.expectedOut))
			n, err := reader.Read(buf)

			if tc.expectedOut == "" {
				require.ErrorIs(t, err, io.EOF, "Expected io.EOF for empty data")

				return
			}

			require.NoError(t, err, "Unexpected read error")

			assert.Equal(t, tc.expectedOut, string(buf[:n]), "Output does not match expected value")
		})
	}
}

func TestReusableReadCloser_ReadThenReset(t *testing.T) {
	t.Parallel()

	input := "hello world"

	rrc, err := hqgohttprequest.NewReusableReadCloser(input)

	require.NoError(t, err)
	require.NotNil(t, rrc)

	buf := make([]byte, len(input))

	n, err := rrc.Read(buf)

	require.NoError(t, err)

	assert.Equal(t, len(input), n)
	assert.Equal(t, input, string(buf[:n]))

	buf2 := make([]byte, len(input))

	n, err = rrc.Read(buf2)

	require.ErrorIs(t, err, io.EOF)
	assert.Zero(t, n)

	rrc.Reset()

	buf3 := make([]byte, len(input))

	n, err = rrc.Read(buf3)

	require.NoError(t, err)

	assert.Equal(t, len(input), n)
	assert.Equal(t, input, string(buf3[:n]))
}

func TestReusableReadCloser_EmptyData(t *testing.T) {
	t.Parallel()

	rrc, err := hqgohttprequest.NewReusableReadCloser([]byte{})

	require.NoError(t, err)
	require.NotNil(t, rrc)

	buf := make([]byte, 10)

	n, err := rrc.Read(buf)

	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, 0, n)
}

func TestReusableReadCloser_Reset(t *testing.T) {
	t.Parallel()

	input := "reset me"

	rrc, err := hqgohttprequest.NewReusableReadCloser(input)

	require.NoError(t, err)
	require.NotNil(t, rrc)

	buf := make([]byte, 5)

	n, err := rrc.Read(buf)

	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, input[:5], string(buf[:n]))

	rrc.Reset()

	buf2 := make([]byte, len(input))

	n, err = rrc.Read(buf2)

	require.NoError(t, err)
	assert.Equal(t, len(input), n)
	assert.Equal(t, input, string(buf2[:n]))
}

func TestReusableReadCloser_PartialRead(t *testing.T) {
	t.Parallel()

	input := "hello world"

	rrc, err := hqgohttprequest.NewReusableReadCloser(input)

	require.NoError(t, err)
	require.NotNil(t, rrc)

	buf1 := make([]byte, 5)

	n, err := rrc.Read(buf1)

	require.NoError(t, err)

	assert.Equal(t, 5, n)
	assert.Equal(t, input[:5], string(buf1[:n]))

	buf2 := make([]byte, 5)

	n, err = rrc.Read(buf2)

	require.NoError(t, err)

	assert.Equal(t, 5, n)
	assert.Equal(t, input[5:10], string(buf2[:n]))

	buf3 := make([]byte, 5)

	n, err = rrc.Read(buf3)

	require.NoError(t, err)

	assert.Equal(t, 1, n)
	assert.Equal(t, input[10:], string(buf3[:n]))

	buf4 := make([]byte, 5)

	n, err = rrc.Read(buf4)

	require.ErrorIs(t, err, io.EOF)
	assert.Zero(t, n)

	rrc.Reset()

	n, err = rrc.Read(buf4)

	require.NoError(t, err)

	assert.Equal(t, 5, n)
	assert.Equal(t, input[:5], string(buf4[:n]))
}

func TestReusableReadCloser_ConcurrentRead(t *testing.T) {
	t.Parallel()

	input := "concurrent"

	rrc, err := hqgohttprequest.NewReusableReadCloser(input)

	require.NoError(t, err)
	require.NotNil(t, rrc)

	var (
		wg    sync.WaitGroup
		total atomic.Int64
	)

	numGoroutines := 10

	for range numGoroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				buf := make([]byte, 1)

				n, err := rrc.Read(buf)
				total.Add(int64(n))

				if err == nil {
					continue
				}

				if !errors.Is(err, io.EOF) {
					t.Errorf("unexpected error: %v", err)
				}

				return
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, int64(len(input)), total.Load(),
		"Expected concurrent readers to consume the payload exactly once")
}

// customReadSeeker implements io.ReadSeeker without being one of the concrete
// reader types NewReusableReadCloser special-cases.
//
// Fields:
//   - Reader (*bytes.Reader): the underlying seekable reader.
type customReadSeeker struct {
	Reader *bytes.Reader
}

// Read delegates to the underlying reader.
//
// Parameters:
//   - p ([]byte): the destination buffer.
//
// Returns:
//   - n (int): the number of bytes read.
//   - err (error): non-nil when the read fails.
func (r customReadSeeker) Read(p []byte) (n int, err error) {
	return r.Reader.Read(p)
}

// Seek delegates to the underlying reader.
//
// Parameters:
//   - offset (int64): the seek offset.
//   - whence (int): the seek origin.
//
// Returns:
//   - position (int64): the new offset.
//   - err (error): non-nil when the seek fails.
func (r customReadSeeker) Seek(offset int64, whence int) (position int64, err error) {
	return r.Reader.Seek(offset, whence)
}

// customReader implements io.Reader without matching a more specific type.
//
// Fields:
//   - Reader (io.Reader): the underlying reader.
type customReader struct {
	Reader io.Reader
}

// Read delegates to the underlying reader.
//
// Parameters:
//   - p ([]byte): the destination buffer.
//
// Returns:
//   - n (int): the number of bytes read.
//   - err (error): non-nil when the read fails.
func (r customReader) Read(p []byte) (n int, err error) {
	return r.Reader.Read(p)
}

func TestReusableReadCloser_Close(t *testing.T) {
	t.Parallel()

	rrc, err := hqgohttprequest.NewReusableReadCloser("closable")

	require.NoError(t, err)

	require.NoError(t, rrc.Close())
}

func TestNewReusableReadCloser_CopiesInputs(t *testing.T) {
	t.Parallel()

	t.Run("byte slice", func(t *testing.T) {
		t.Parallel()

		input := []byte("original")

		rrc, err := hqgohttprequest.NewReusableReadCloser(input)

		require.NoError(t, err)

		copy(input, "mutated!")

		body, err := io.ReadAll(rrc)

		require.NoError(t, err)
		assert.Equal(t, "original", string(body))
	})

	t.Run("pointer to byte slice", func(t *testing.T) {
		t.Parallel()

		input := []byte("original")

		rrc, err := hqgohttprequest.NewReusableReadCloser(&input)

		require.NoError(t, err)

		copy(input, "mutated!")

		body, err := io.ReadAll(rrc)

		require.NoError(t, err)
		assert.Equal(t, "original", string(body))
	})

	t.Run("bytes buffer", func(t *testing.T) {
		t.Parallel()

		input := bytes.NewBufferString("original")

		rrc, err := hqgohttprequest.NewReusableReadCloser(input)

		require.NoError(t, err)

		input.Reset()
		_, err = input.WriteString("mutated!")

		require.NoError(t, err)

		body, err := io.ReadAll(rrc)

		require.NoError(t, err)
		assert.Equal(t, "original", string(body))
	})
}

func TestReusableReadCloser_ZeroValue(t *testing.T) {
	t.Parallel()

	var closer hqgohttprequest.ReusableReadCloser

	closer.Reset()

	n, err := closer.Read(make([]byte, 1))

	assert.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)

	closer.Reset()

	req, err := hqgohttprequest.New(t.Context(), "POST", "http://example.com", &closer)

	require.NoError(t, err)
	assert.Zero(t, req.ContentLength)
}
