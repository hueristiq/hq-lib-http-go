// Package header provides a strongly typed collection of HTTP header fields as
// defined by IANA, along with helpers for canonicalizing header keys.
//
// It suits code that sets, reads, or manipulates HTTP headers — clients,
// servers, middleware built on net/http — where a plain string invites typos
// that compile fine and fail at runtime.
//
// # Entry points
//
// The [Header] type is a string alias with a String method, and the package
// constants cover the standard header fields registered with IANA plus common
// non-standard ones: CORS, caching, security, content negotiation, and
// connection management.
//
// [CanonicalizeHeaderKey] converts a header key to its canonical form (for
// example, "content-type" to "Content-Type"), as recommended by RFC 9110.
//
// Link header parsing lives in the subpackage
// [github.com/hueristiq/hq-lib-http-go/header/link].
//
// The implementation relies on the Go standard library only.
package header
