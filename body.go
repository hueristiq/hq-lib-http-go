package http

import (
	"context"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// responseBody ties a handed-out body to the execution lifecycle.
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
