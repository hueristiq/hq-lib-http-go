package httpclient

import (
	hqgoerrors "github.com/hueristiq/hq-lib-errors-go"
)

// Sentinel errors classify failures callers can inspect with errors.Is.
const (
	// ErrNilRequest rejects a nil request passed to [Client.Do].
	ErrNilRequest hqgoerrors.Sentinel = "request is nil"

	// ErrNilRequestURL rejects a request whose URL is nil.
	ErrNilRequestURL hqgoerrors.Sentinel = "request URL is nil"

	// ErrBodyNotReplayable reports a body that cannot be resent because the
	// request has no GetBody function (or GetBody failed).
	ErrBodyNotReplayable hqgoerrors.Sentinel = "request body cannot be replayed"

	// ErrHTTP3ClientRequired reports an HTTP/3 protocol selection without an
	// injected [WithHTTP3Client] client.
	ErrHTTP3ClientRequired hqgoerrors.Sentinel = "HTTP/3 requires WithHTTP3Client"

	// ErrHTTP3ProxyUnsupported rejects a proxied request in HTTP/3-only mode;
	// HTTP/3 transports do not honor HTTP proxies.
	ErrHTTP3ProxyUnsupported hqgoerrors.Sentinel = "HTTP/3 does not support proxies"

	// ErrRetriesExhausted matches a [*RetryError] produced when the attempt
	// budget ran out (unless the cause was an unreplayable body).
	ErrRetriesExhausted hqgoerrors.Sentinel = "retries exhausted"

	// ErrClientClosed rejects executions on a closed client and cancels
	// in-flight ones.
	ErrClientClosed hqgoerrors.Sentinel = "client is closed"
)
