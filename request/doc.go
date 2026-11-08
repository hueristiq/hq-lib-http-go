// Package request provides an in-memory, rewindable request body for callers
// that need a custom replay implementation. Standard http.Request values built
// from bytes.Buffer, bytes.Reader, or strings.Reader already support GetBody.
//
// # Entry points
//
//   - [NewReusableReadCloser] buffers any supported input — []byte, string,
//     *bytes.Buffer, or any io.Reader — into a [ReusableReadCloser] whose
//     [ReusableReadCloser.Reset] rewinds it for a retry or redirect resend.
//   - [NewReusableReadCloserLimited] does the same while refusing to buffer
//     more than a configured limit, reporting [ErrBodyTooLarge]; unsupported
//     input types report [ErrUnsupportedBodyType].
//
// A ReusableReadCloser exposes GetBody-style replay: its zero value reads as
// an empty payload, and a nil pointer is safe to read and reset. The
// implementation relies on the Go standard library only.
package request
