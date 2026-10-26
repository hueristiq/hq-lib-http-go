package request

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
)

// ReusableReadCloser implements the io.ReadCloser interface by wrapping an
// in‑memory byte slice and a bytes.Reader. Reads follow the io.Reader contract:
// the closer returns io.EOF when the data is exhausted. Call [Reset] to read
// the payload again.
//
// An *http.Request built with [New] exposes a GetBody function that returns a
// fresh closer, so retries and redirects replay the full payload without
// relying on the reader's position.
//
// The zero value is usable and reads as an empty payload. A nil
// *ReusableReadCloser is also safe: Read returns io.EOF and Reset is a no-op.
//
// Fields:
//   - mu (sync.Mutex): guards the reader position.
//   - data ([]byte): the immutable payload served by every read cycle.
//   - reader (*bytes.Reader): the cursor over data.
type ReusableReadCloser struct {
	mu     sync.Mutex
	data   []byte
	reader *bytes.Reader
}

// Read reads up to len(p) bytes from the internal data into the provided
// buffer p. It implements the io.Reader interface and returns io.EOF once the
// data is exhausted, matching the convention every generic reader consumer
// expects. Call [Reset] to read the payload again.
//
// Concurrency:
//   - The method employs a mutex (mu) to guarantee that concurrent calls to Read are thread-safe.
//
// Parameters:
//   - p ([]byte): the buffer into which data is to be read.
//
// Returns:
//   - n (int): the number of bytes successfully read into the buffer p.
//   - err (error): io.EOF once the data is exhausted, or a read error from the
//     underlying reader.
func (r *ReusableReadCloser) Read(p []byte) (n int, err error) {
	if r == nil {
		return 0, io.EOF
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.reader == nil {
		r.reader = bytes.NewReader(r.data)
	}

	n, err = r.reader.Read(p)

	return n, err
}

// Reset rewinds the reader to the beginning of the data, so the next Read
// returns the payload from the start. The client calls it to resend a body on
// a retry or an HTTP/2 fallback attempt.
func (r *ReusableReadCloser) Reset() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.reset()
}

// clone returns an independent closer over the same immutable payload,
// positioned at the start. It backs the GetBody function of requests built by
// [New].
//
// Returns:
//   - clone (*ReusableReadCloser): a fresh closer over the same data.
func (r *ReusableReadCloser) clone() (clone *ReusableReadCloser) {
	if r == nil {
		return &ReusableReadCloser{reader: bytes.NewReader(nil)}
	}

	clone = &ReusableReadCloser{
		data:   r.data,
		reader: bytes.NewReader(r.data),
	}

	return clone
}

// reset repositions the internal bytes.Reader to the beginning of the data.
// Any errors from Seek are ignored because the operation is guaranteed to
// succeed with an in‑memory data source.
//
// The caller must hold r.mu.
func (r *ReusableReadCloser) reset() {
	if r == nil {
		return
	}

	if r.reader == nil {
		r.reader = bytes.NewReader(r.data)

		return
	}

	_, _ = r.reader.Seek(0, io.SeekStart)
}

// Close implements the io.Closer interface. Since ReusableReadCloser operates solely
// on in-memory data and does not manage external resources, Close is a no-op that always returns nil.
//
// Returns:
//   - err (error): Always nil.
func (r *ReusableReadCloser) Close() (err error) {
	return nil
}

// ErrUnsupportedBodyType reports an input type that [NewReusableReadCloser]
// cannot convert into a request body.
var ErrUnsupportedBodyType = errors.New("unsupported body type")

// ErrBodyTooLarge reports a request body that exceeds the configured buffering
// limit. It is returned by [NewReusableReadCloserLimited] and
// [NewLimited].
var ErrBodyTooLarge = errors.New("request body exceeds the configured limit")

// NewReusableReadCloser creates a new instance of ReusableReadCloser from a variety of input data types.
// The function converts the provided input into an in‑memory byte slice and initializes a bytes.Reader,
// enabling repeated reads of the same data.
//
// Supported input types via type assertion include:
//   - nil: Results in an empty byte slice.
//   - []byte: Copies the provided byte slice.
//   - *[]byte: Dereferences the pointer and copies the byte slice.
//   - string: Converts the string into a byte slice.
//   - *bytes.Buffer: Copies the buffer's underlying bytes.
//   - *bytes.Reader, *strings.Reader, *io.SectionReader, io.ReadSeeker, io.Reader:
//     Reads the entire content into a byte slice using io.ReadAll.
//
// The payload is owned by the returned closer. Later mutations of a []byte,
// *[]byte, or *bytes.Buffer argument do not change the request body.
//
// Parameters:
//   - raw (any): the input data to be converted to a byte slice. The function handles
//     the conversion based on the dynamic type of the input.
//
// Returns:
//   - reusableReadCloser (*ReusableReadCloser): a pointer to the newly created ReusableReadCloser instance.
//   - err (error): an error if the input type is unsupported or if an error occurs during data reading.
func NewReusableReadCloser(raw any) (reusableReadCloser *ReusableReadCloser, err error) {
	reusableReadCloser, err = NewReusableReadCloserLimited(raw, 0)

	return reusableReadCloser, err
}

// NewReusableReadCloserLimited creates a [ReusableReadCloser] from raw while
// refusing to buffer more than limit bytes. A non-positive limit means
// unlimited.
//
// The input types are the same as [NewReusableReadCloser]. The limit is checked
// before copying byte slices, strings, and buffers, and while reading an
// io.Reader, so an oversized payload is rejected without being fully retained.
//
// Parameters:
//   - raw (any): the input data to convert.
//   - limit (int64): the maximum payload size in bytes; non-positive means
//     unlimited.
//
// Returns:
//   - reusableReadCloser (*ReusableReadCloser): the buffered body.
//   - err (error): [ErrBodyTooLarge] when the payload exceeds the limit,
//     [ErrUnsupportedBodyType] for an unsupported type, or a read error.
func NewReusableReadCloserLimited(raw any, limit int64) (reusableReadCloser *ReusableReadCloser, err error) {
	var data []byte

	switch v := raw.(type) {
	case nil:
		data = []byte{}
	case []byte:
		if exceedsLimit(int64(len(v)), limit) {
			return nil, ErrBodyTooLarge
		}

		data = bytes.Clone(v)
	case *[]byte:
		if v != nil {
			if exceedsLimit(int64(len(*v)), limit) {
				return nil, ErrBodyTooLarge
			}

			data = bytes.Clone(*v)
		}
	case string:
		if exceedsLimit(int64(len(v)), limit) {
			return nil, ErrBodyTooLarge
		}

		data = []byte(v)
	case *bytes.Buffer:
		if v != nil {
			if exceedsLimit(int64(v.Len()), limit) {
				return nil, ErrBodyTooLarge
			}

			data = bytes.Clone(v.Bytes())
		}
	case *bytes.Reader, *strings.Reader, *io.SectionReader, io.ReadSeeker, io.Reader:
		var r io.Reader

		switch rr := v.(type) {
		case *bytes.Reader:
			r = rr
		case *strings.Reader:
			r = rr
		case *io.SectionReader:
			r = rr
		case io.ReadSeeker:
			r = rr
		case io.Reader:
			r = rr
		}

		if limit > 0 && limit != math.MaxInt64 {
			data, err = io.ReadAll(io.LimitReader(r, limit+1))
		} else {
			data, err = io.ReadAll(r)
		}

		if err != nil {
			return nil, err
		}

		if exceedsLimit(int64(len(data)), limit) {
			return nil, ErrBodyTooLarge
		}
	default:
		err = fmt.Errorf("%w: %T", ErrUnsupportedBodyType, v)

		return nil, err
	}

	reusableReadCloser = &ReusableReadCloser{
		data:   data,
		reader: bytes.NewReader(data),
	}

	return reusableReadCloser, nil
}

// exceedsLimit reports whether size is over a positive limit.
//
// Parameters:
//   - size (int64): the payload size.
//   - limit (int64): the maximum allowed size; non-positive means unlimited.
//
// Returns:
//   - exceeds (bool): true when size is greater than a positive limit.
func exceedsLimit(size, limit int64) (exceeds bool) {
	exceeds = limit > 0 && size > limit

	return exceeds
}
