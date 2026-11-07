// Package request provides an in-memory, rewindable request body for callers
// that need a custom replay implementation. Standard http.Request values built
// from bytes.Buffer, bytes.Reader, or strings.Reader already support GetBody.
package request
