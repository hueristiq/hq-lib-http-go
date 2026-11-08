package http

import (
	"context"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// responseBody ties a handed-out body to the execution lifecycle: closing it
// — or hitting EOF — releases the execution, and the body-read timeout
// surfaces as the read error's cause.
//
// Fields:
//   - ReadCloser (io.ReadCloser): the underlying response body.
//   - cancel (context.CancelCauseFunc): cancels the execution context.
//   - attemptCancel (context.CancelCauseFunc): cancels the last attempt's
//     context, or nil.
//   - cause (func() error): reports the execution context's cancellation
//     cause, so a timeout reads as context.DeadlineExceeded.
//   - stopCloseWatch (func() bool): detaches the client-close watcher.
//   - timer (*time.Timer): the body-read timeout, or nil.
//   - closeOnce (sync.Once): closes the underlying body exactly once.
//   - closeErr (error): the memoized close error.
//   - cancelOnce (sync.Once): releases the execution exactly once.
//   - closed (atomic.Bool): set by Close; reads after close return the
//     underlying error rather than the cancellation cause.
type responseBody struct {
	io.ReadCloser
	cancel         context.CancelCauseFunc
	attemptCancel  context.CancelCauseFunc
	cause          func() error
	stopCloseWatch func() bool
	timer          *time.Timer
	closeOnce      sync.Once
	closeErr       error
	cancelOnce     sync.Once
	closed         atomic.Bool
}

var _ io.ReadCloser = (*responseBody)(nil)

// Read reads from the underlying body. A read failure ends the body's hold
// on the execution: EOF and post-close errors pass through, while a canceled
// execution surfaces its cause (a body timeout reads as
// context.DeadlineExceeded).
//
// Parameters:
//   - p ([]byte): the destination buffer.
//
// Returns:
//   - n (int): the number of bytes read.
//   - err (error): the read error, or the cancellation cause.
func (b *responseBody) Read(p []byte) (n int, err error) {
	n, err = b.ReadCloser.Read(p)
	if err == nil {
		return n, nil
	}

	if b.timer != nil {
		b.timer.Stop()
	}

	cause := b.cause()
	b.release()

	if err == io.EOF || b.closed.Load() {
		return n, err
	}

	if cause != nil {
		return n, cause
	}

	return n, err
}

// release frees the execution exactly once: it cancels the attempt context,
// detaches the client-close watcher, and cancels the execution context.
func (b *responseBody) release() {
	b.cancelOnce.Do(func() {
		if b.attemptCancel != nil {
			b.attemptCancel(nil)
		}

		if b.stopCloseWatch != nil {
			b.stopCloseWatch()
		}

		b.cancel(nil)
	})
}

// Close releases the execution and closes the underlying body exactly once;
// later calls return the first close's result.
//
// Returns:
//   - err (error): the underlying body's close error.
func (b *responseBody) Close() (err error) {
	b.closed.Store(true)

	if b.timer != nil {
		b.timer.Stop()
	}

	b.release()

	b.closeOnce.Do(func() {
		b.closeErr = b.ReadCloser.Close()
	})

	return b.closeErr
}

// drainBody discards up to limit bytes from body and closes it, so a retried
// response frees its connection for reuse.
//
// Parameters:
//   - body (io.ReadCloser): the body to drain; nil is a no-op.
//   - limit (int64): the maximum number of bytes to discard.
func drainBody(body io.ReadCloser, limit int64) {
	if body == nil {
		return
	}

	defer func() {
		_ = body.Close()
	}()

	if limit < math.MaxInt64 {
		limit++
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(body, limit))
}
