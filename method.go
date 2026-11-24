package httpclient

// Method represents an HTTP method. Conversion from any string is allowed;
// the type does not validate method names. The constants cover the common
// methods registered by IANA and used by HTTP tooling; MethodQuery is the
// QUERY method net/http does not define.
type Method string

// String returns the underlying string representation of the Method.
func (m Method) String() (method string) {
	method = string(m)

	return method
}

// Predefined HTTP method constants.
const (
	MethodConnect Method = "CONNECT"
	MethodDelete  Method = "DELETE"
	MethodGet     Method = "GET"
	MethodHead    Method = "HEAD"
	MethodOptions Method = "OPTIONS"
	MethodPatch   Method = "PATCH"
	MethodPost    Method = "POST"
	MethodPut     Method = "PUT"
	MethodQuery   Method = "QUERY"
	MethodTrace   Method = "TRACE"
)
