// Package status models HTTP status codes as a typed value.
//
// It suits code that branches on response status codes, where comparing raw
// integers obscures intent and invites magic numbers.
//
// # Entry points
//
// The [Status] type is a defined int type: Int returns the numeric code, String
// the standard reason phrase, and the Is* methods — IsInformational, IsSuccess,
// IsRedirection, IsClientError, IsServerError, IsError — classify a code by its
// response class. Conversion from any int is allowed; the type does not validate
// the code. The constants cover the standard codes from 100 Continue through 511
// Network Authentication Required.
//
// The implementation relies on the Go standard library only.
package status
