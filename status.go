package httpclient

import (
	"fmt"
	"net/http"
)

// Status represents an HTTP status code. Conversion from any int is allowed;
// the type does not validate the code. It provides the numeric value, the
// standard reason phrase, and helpers for classifying the response.
type Status int

// Int returns the integer (numeric) representation of the Status instance.
func (s Status) Int() (status int) {
	status = int(s)

	return status
}

// String returns the standard reason phrase for the status code, or
// "Unknown Status (<code>)" for a code net/http does not know.
func (s Status) String() (status string) {
	status = http.StatusText(s.Int())

	if status == "" {
		status = fmt.Sprintf("Unknown Status (%d)", s.Int())
	}

	return status
}

// IsInformational reports whether the status code is informational (1xx).
func (s Status) IsInformational() (isInformational bool) {
	return s >= 100 && s < 200
}

// IsSuccess reports whether the status code indicates success (2xx).
func (s Status) IsSuccess() (isSuccess bool) {
	return s >= 200 && s < 300
}

// IsRedirection reports whether the status code indicates a redirection (3xx).
func (s Status) IsRedirection() (isRedirection bool) {
	return s >= 300 && s < 400
}

// IsError reports whether the status code is a client (4xx) or server (5xx) error.
func (s Status) IsError() (isError bool) {
	return s.IsClientError() || s.IsServerError()
}

// IsClientError reports whether the status code indicates a client error (4xx).
func (s Status) IsClientError() (isClientError bool) {
	return s >= 400 && s < 500
}

// IsServerError reports whether the status code indicates a server error (5xx).
func (s Status) IsServerError() (isServerError bool) {
	return s >= 500 && s < 600
}

// Predefined Status type constants.
//
// The following constants define standard HTTP status codes, grouped by
// category.
const (
	StatusContinue                      Status = 100
	StatusSwitchingProtocols            Status = 101
	StatusProcessing                    Status = 102
	StatusEarlyHints                    Status = 103
	StatusOK                            Status = 200
	StatusCreated                       Status = 201
	StatusAccepted                      Status = 202
	StatusNonAuthoritativeInfo          Status = 203
	StatusNoContent                     Status = 204
	StatusResetContent                  Status = 205
	StatusPartialContent                Status = 206
	StatusMultiStatus                   Status = 207
	StatusAlreadyReported               Status = 208
	StatusIMUsed                        Status = 226
	StatusMultipleChoices               Status = 300
	StatusMovedPermanently              Status = 301
	StatusFound                         Status = 302
	StatusSeeOther                      Status = 303
	StatusNotModified                   Status = 304
	StatusUseProxy                      Status = 305
	StatusTemporaryRedirect             Status = 307
	StatusPermanentRedirect             Status = 308
	StatusBadRequest                    Status = 400
	StatusUnauthorized                  Status = 401
	StatusPaymentRequired               Status = 402
	StatusForbidden                     Status = 403
	StatusNotFound                      Status = 404
	StatusMethodNotAllowed              Status = 405
	StatusNotAcceptable                 Status = 406
	StatusProxyAuthRequired             Status = 407
	StatusRequestTimeout                Status = 408
	StatusConflict                      Status = 409
	StatusGone                          Status = 410
	StatusLengthRequired                Status = 411
	StatusPreconditionFailed            Status = 412
	StatusRequestEntityTooLarge         Status = 413
	StatusRequestURITooLong             Status = 414
	StatusUnsupportedMediaType          Status = 415
	StatusRequestedRangeNotSatisfiable  Status = 416
	StatusExpectationFailed             Status = 417
	StatusTeapot                        Status = 418
	StatusMisdirectedRequest            Status = 421
	StatusUnprocessableEntity           Status = 422
	StatusLocked                        Status = 423
	StatusFailedDependency              Status = 424
	StatusTooEarly                      Status = 425
	StatusUpgradeRequired               Status = 426
	StatusPreconditionRequired          Status = 428
	StatusTooManyRequests               Status = 429
	StatusRequestHeaderFieldsTooLarge   Status = 431
	StatusUnavailableForLegalReasons    Status = 451
	StatusInternalServerError           Status = 500
	StatusNotImplemented                Status = 501
	StatusBadGateway                    Status = 502
	StatusServiceUnavailable            Status = 503
	StatusGatewayTimeout                Status = 504
	StatusHTTPVersionNotSupported       Status = 505
	StatusVariantAlsoNegotiates         Status = 506
	StatusInsufficientStorage           Status = 507
	StatusLoopDetected                  Status = 508
	StatusNotExtended                   Status = 510
	StatusNetworkAuthenticationRequired Status = 511
)
