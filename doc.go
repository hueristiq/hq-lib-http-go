// Package http provides a resilient HTTP client around standard requests and
// responses.
//
// [New] creates a [Client]. Build requests with [http.NewRequestWithContext]
// and execute them with [Client.Do]:
//
//	client, err := http.New()
//	if err != nil {
//		return err
//	}
//	defer client.Close()
//
//	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
//	if err != nil {
//		return err
//	}
//
//	res, err := client.Do(req)
//	if err != nil {
//		return err
//	}
//	defer res.Body.Close()
//
// The package name collides with net/http, so applications normally import it
// as hqgohttp and use the net/http request constructors and types.
//
// # Defaults
//
// The default client pools HTTP/1.1 and HTTP/2 connections, uses environment
// proxies, sends [DefaultUserAgent], and bounds each execution through response
// headers by [DefaultTimeout]. It makes three attempts for idempotent requests
// with exponential backoff and jitter. The default retry policy covers
// transient transport errors, 408, 429, and 5xx except 501. Response bodies
// have no separate timeout by default; their request context still applies.
//
// [Client.Do] follows net/http status semantics: a final 4xx or 5xx is a
// response with a nil error. The caller owns every returned body. Transport
// exhaustion returns [RetryError] with the last failure and attempt history.
// A body that cannot be replayed through [http.Request.GetBody] is not resent.
// If that happens after a response, the client returns the response; if it
// happens after a transport failure, the error matches [ErrBodyNotReplayable].
//
// # Context and ownership
//
// Context belongs on each request. The client does not store a default request
// context. [WithTimeout] bounds work through response headers, while
// [WithResponseBodyTimeout] independently bounds body reads. Closing or
// draining a body releases execution resources. [Client.Close] cancels active
// requests and rejects new ones; [Client.CloseIdleConnections] leaves the
// client usable.
//
// # Transport and protocols
//
// [WithProtocol] selects HTTP/1.x, HTTP/2, or HTTP/3 modes. HTTP/3 uses a client
// supplied through [WithHTTP3Client], keeping QUIC dependencies outside this
// module. [WithHTTPClient] injects a complete custom client for standard
// protocols. [WithTransportWrapper] decorates library-built transports.
// Contradictory combinations fail during [New] instead of being ignored.
//
// # Extension contracts
//
// [RateLimiter], [RetryPolicy], [Logger], and the callback options are explicit
// in-process extension seams. Implementations shared across clients must be
// safe for concurrent use. Callbacks run synchronously. Response observers must
// not consume bodies. Retry hooks may inspect only status and headers after a
// failed body has been drained. [PrepareRetryFunc] may update a request before
// the next attempt and may return an error to stop execution.
package http
