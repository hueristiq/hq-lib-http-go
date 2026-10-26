// Package mime provides a typed representation of Internet Media Types (MIME
// types).
//
// It suits code that sets or inspects Content-Type and Accept headers, where a
// plain string invites typos that compile fine and fail at runtime.
//
// # Entry points
//
// The [MIME] type is a defined string type with a String method; conversion from
// any string is allowed, and the type does not validate values. The constants
// cover common registered and de facto media types.
//
// The implementation relies on the Go standard library only.
package mime
