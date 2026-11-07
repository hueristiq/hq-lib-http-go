package http

import "errors"

// Sentinel errors classify failures callers can inspect with errors.Is.
var (
	ErrNilRequest            = errors.New("request is nil")
	ErrNilRequestURL         = errors.New("request URL is nil")
	ErrBodyNotReplayable     = errors.New("request body cannot be replayed")
	ErrHTTP3ClientRequired   = errors.New("HTTP/3 requires WithHTTP3Client")
	ErrHTTP3ProxyUnsupported = errors.New("HTTP/3 does not support proxies")
	ErrRetriesExhausted      = errors.New("retries exhausted")
	ErrClientClosed          = errors.New("client is closed")
)
