// Package method provides a strongly typed representation of HTTP methods as
// defined by IANA.
//
// It suits code that constructs or compares HTTP methods, where a plain string
// invites typos that compile fine and fail at runtime.
//
// # Entry points
//
// The [Method] type is a string alias with a String method, and the package
// constants cover the standard methods: GET, HEAD, POST, PUT, PATCH, DELETE,
// OPTIONS, CONNECT, TRACE, and QUERY (RFC 10008).
//
// The implementation relies on the Go standard library only.
package method
