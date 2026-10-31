// Package request wraps an HTTP request with a reusable body.
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
// becoming the request's ContentLength. A supplied *ReusableReadCloser is
// cloned, and a reader implementing io.Closer is closed after buffering. A nil
// ctx leaves the client default in charge; an explicit context.Background
// overrides it.
//
// The request's GetBody function returns a fresh reader over the payload, and
// [ReusableReadCloser.Reset] rewinds an existing reader. Together they let
// retries, redirects, and repeated executions resend the full body.
// [ReusableReadCloser.Read] follows the io.Reader contract and returns
// io.EOF when the data is exhausted. The embedded standard-library request is
// available through [Request.Std].
//
// URLs are parsed with [net/url.Parse] by default, which is strict: it applies
// no default scheme and repairs no invalid percent escapes. [WithURLParser]
// supplies a custom [URLParser] — for example an adapter around
// github.com/hueristiq/hq-lib-url-go/parser built with [URLParserFunc] — that
// can add a default scheme, repair escapes, or apply any other transformation:
//
//	urlParser := urlparser.New(
//		urlparser.WithDefaultScheme("http"),
//		urlparser.WithLenient(),
//	)
//
//	req, err := request.New(ctx, method, rawURL, body,
//		request.WithURLParser(request.URLParserFunc(func(raw string) (*url.URL, error) {
//			parsed, err := urlParser.Parse(raw)
//			if err != nil {
//				return nil, err
//			}
//
//			return parsed.URL, nil
//		})),
//	)
//
// The implementation relies on the Go standard library only. The URL parser is
// injected by the caller, so no dependency on hq-lib-url-go is imposed.
package request
