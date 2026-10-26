package status

import (
	"fmt"
	"net/http"
)

// Status represents HTTP status codes as defined by the IANA registry.
//
// As an integer alias, Status allows developers to work with HTTP status codes
// in a type-safe manner. In addition to retrieving the raw numeric code,
// it provides methods for obtaining a human-readable description and for
// categorizing the code by its response class.
type Status int

// Int returns the integer (numeric) representation of the Status instance.
//
// This method extracts the underlying integer value from a Status value,
// corresponding to the actual HTTP status code (e.g., 200, 404, etc.).
//
// Returns:
//   - status (int): the numeric HTTP status code.
func (s Status) Int() (status int) {
	status = int(s)

	return status
}

// String returns the human-readable description of the HTTP status code.
//
// The description comes from net/http's StatusText, the standard reason
// phrases. If an unknown status code is encountered, the method returns a
// formatted string indicating that the code is unknown.
//
// Returns:
//   - status (string): the descriptive text for the HTTP status code.
func (s Status) String() (status string) {
	status = http.StatusText(s.Int())

	if status == "" {
		status = fmt.Sprintf("Unknown Status (%d)", s.Int())
	}

	return status
}

// IsInformational checks if the status code is an informational response (1xx).
//
// Informational responses (100–199) indicate that the request has been received
// and is being processed, but no final response is yet available.
//
// Returns:
//   - isInformational (bool): true if s is between 100 and 199, false otherwise.
func (s Status) IsInformational() (isInformational bool) {
	return s >= 100 && s < 200
}

// IsSuccess checks if the status code indicates a successful response (2xx).
//
// Success responses (200–299) indicate that the request was successfully
// received, understood, and accepted by the server.
//
// Returns:
//   - isSuccess (bool): true if s is between 200 and 299, false otherwise.
func (s Status) IsSuccess() (isSuccess bool) {
	return s >= 200 && s < 300
}

// IsRedirection checks if the status code indicates a redirection (3xx).
//
// Redirection responses (300–399) indicate that further action is needed to
// fulfill the request, usually involving a change in URL or method.
//
// Returns:
//   - isRedirection (bool): true if s is between 300 and 399, false otherwise.
func (s Status) IsRedirection() (isRedirection bool) {
	return s >= 300 && s < 400
}

// IsError checks if the status code represents an error (either client or
// server error).
//
// A status code is considered an error if it is either a client error (4xx) or
// a server error (5xx).
//
// Returns:
//   - isError (bool): true if s is in the 4xx or 5xx range, false otherwise.
func (s Status) IsError() (isError bool) {
	return s.IsClientError() || s.IsServerError()
}

// IsClientError checks if the status code indicates a client error (4xx).
//
// Client error responses (400–499) indicate that the client sent an invalid
// request.
//
// Returns:
//   - isClientError (bool): true if s is between 400 and 499, false otherwise.
func (s Status) IsClientError() (isClientError bool) {
	return s >= 400 && s < 500
}

// IsServerError checks if the status code indicates a server error (5xx).
//
// Server error responses (500–599) indicate that the server failed to fulfill a
// valid request.
//
// Returns:
//   - isServerError (bool): true if s is between 500 and 599, false otherwise.
func (s Status) IsServerError() (isServerError bool) {
	return s >= 500 && s < 600
}

// Predefined Status type constants.
//
// The following constants define standard HTTP status codes, grouped by
// category.
const (
	Continue                      Status = 100
	SwitchingProtocols            Status = 101
	Processing                    Status = 102
	EarlyHints                    Status = 103
	OK                            Status = 200
	Created                       Status = 201
	Accepted                      Status = 202
	NonAuthoritativeInfo          Status = 203
	NoContent                     Status = 204
	ResetContent                  Status = 205
	PartialContent                Status = 206
	MultiStatus                   Status = 207
	AlreadyReported               Status = 208
	IMUsed                        Status = 226
	MultipleChoices               Status = 300
	MovedPermanently              Status = 301
	Found                         Status = 302
	SeeOther                      Status = 303
	NotModified                   Status = 304
	UseProxy                      Status = 305
	_                             Status = 306
	TemporaryRedirect             Status = 307
	PermanentRedirect             Status = 308
	BadRequest                    Status = 400
	Unauthorized                  Status = 401
	PaymentRequired               Status = 402
	Forbidden                     Status = 403
	NotFound                      Status = 404
	MethodNotAllowed              Status = 405
	NotAcceptable                 Status = 406
	ProxyAuthRequired             Status = 407
	RequestTimeout                Status = 408
	Conflict                      Status = 409
	Gone                          Status = 410
	LengthRequired                Status = 411
	PreconditionFailed            Status = 412
	RequestEntityTooLarge         Status = 413
	RequestURITooLong             Status = 414
	UnsupportedMediaType          Status = 415
	RequestedRangeNotSatisfiable  Status = 416
	ExpectationFailed             Status = 417
	Teapot                        Status = 418
	MisdirectedRequest            Status = 421
	UnprocessableEntity           Status = 422
	Locked                        Status = 423
	FailedDependency              Status = 424
	TooEarly                      Status = 425
	UpgradeRequired               Status = 426
	PreconditionRequired          Status = 428
	TooManyRequests               Status = 429
	RequestHeaderFieldsTooLarge   Status = 431
	UnavailableForLegalReasons    Status = 451
	InternalServerError           Status = 500
	NotImplemented                Status = 501
	BadGateway                    Status = 502
	ServiceUnavailable            Status = 503
	GatewayTimeout                Status = 504
	HTTPVersionNotSupported       Status = 505
	VariantAlsoNegotiates         Status = 506
	InsufficientStorage           Status = 507
	LoopDetected                  Status = 508
	NotExtended                   Status = 510
	NetworkAuthenticationRequired Status = 511
)
