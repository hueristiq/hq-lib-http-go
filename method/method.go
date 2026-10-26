package method

// Method represents an HTTP method.
//
// Method is a defined string type. Conversion from any string is allowed; the
// type does not validate method names. The constants below cover the common
// methods registered by IANA and used by HTTP tooling.
type Method string

// String returns the underlying string representation of the Method.
//
// This method converts the Method (a string alias) to its plain string value. This conversion is
// particularly useful when the method needs to be included in HTTP requests, log messages, or debug output.
//
// Returns:
//   - method (string): the HTTP method as a string.
func (m Method) String() (method string) {
	method = string(m)

	return method
}

// Predefined HTTP method constants.
//
// These constants cover the common HTTP methods and are declared as type Method
// to make their intent clear and prevent misspellings. Although some of these
// methods (such as POST, PUT, DELETE, and PATCH) may change server state, they
// are included to cover the common HTTP operations.
const (
	CONNECT Method = "CONNECT"
	DELETE  Method = "DELETE"
	GET     Method = "GET"
	HEAD    Method = "HEAD"
	OPTIONS Method = "OPTIONS"
	PATCH   Method = "PATCH"
	POST    Method = "POST"
	PUT     Method = "PUT"
	QUERY   Method = "QUERY"
	TRACE   Method = "TRACE"
)
