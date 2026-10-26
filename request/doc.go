// Package request provides an HTTP request wrapper with a reusable body.
//
// It suits scenarios where the same request is sent repeatedly — retries,
// logging, proxying — and reading the body must not consume it.
//
// # Entry points
//
// [New] builds a [Request] wrapping a standard *http.Request, buffering the
// body in memory as a [ReusableReadCloser]. The body accepts a string, a
// []byte, an io.Reader, an io.ReadSeeker, a *bytes.Buffer, a
// func() (io.Reader, error), or a *ReusableReadCloser, with its length
// becoming the request's ContentLength. A nil ctx means
// context.Background.
//
// The request's GetBody function returns a fresh reader over the payload, and
// [ReusableReadCloser.Reset] rewinds an existing reader. Together they let
// retries, redirects, and repeated executions resend the full body.
// [ReusableReadCloser.Read] follows the io.Reader contract and returns
// io.EOF when the data is exhausted. The embedded standard-library request is
// available through [Request.Std].
//
// URLs are parsed with a lenient parser that applies the default scheme "http"
// when none is given, so "example.com/path" is a valid URL. Invalid percent
// escapes are repaired (a "%" that does not start a two-digit escape becomes
// "%25"), while valid escapes and raw query text are preserved.
//
// The implementation relies on the Go standard library, plus
// github.com/hueristiq/hq-lib-url-go (URL parsing with scheme defaulting).
package request
