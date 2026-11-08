package http

import "errors"

// Sentinel errors classify failures callers can inspect with errors.Is.
var (
	// ErrNilRequest rejects a nil request passed to [Client.Do].
	ErrNilRequest = errors.New("request is nil")

	// ErrNilRequestURL rejects a request whose URL is nil.
	ErrNilRequestURL = errors.New("request URL is nil")

	// ErrBodyNotReplayable reports a body that cannot be resent because the
	// request has no GetBody function (or GetBody failed).
	ErrBodyNotReplayable = errors.New("request body cannot be replayed")

	// ErrHTTP3ClientRequired reports an HTTP/3 protocol selection without an
	// injected [WithHTTP3Client] client.
	ErrHTTP3ClientRequired = errors.New("HTTP/3 requires WithHTTP3Client")

	// ErrHTTP3ProxyUnsupported rejects a proxied request in HTTP/3-only mode;
	// HTTP/3 transports do not honor HTTP proxies.
	ErrHTTP3ProxyUnsupported = errors.New("HTTP/3 does not support proxies")

	// ErrRetriesExhausted matches a [*RetryError] produced when the attempt
	// budget ran out (unless the cause was an unreplayable body).
	ErrRetriesExhausted = errors.New("retries exhausted")

	// ErrClientClosed rejects executions on a closed client and cancels
	// in-flight ones.
	ErrClientClosed = errors.New("client is closed")
)
