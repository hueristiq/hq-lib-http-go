package http

import (
	"net/url"

	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
)

// Request creates a [RequestBuilder] executing through the client.
//
// Parameters:
//
//	none.
//
// Returns:
//   - builder (*RequestBuilder): a new request builder.
func (c *Client) Request() (builder *RequestBuilder) {
	builder = &RequestBuilder{
		client: c,
		params: make(url.Values),
	}

	return builder
}

// Get is a convenience method for performing an HTTP GET request.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Get(rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.GET).URL(rawURL).WithOptions(ofs...).Do()

	return res, err
}

// Head is a convenience method for performing an HTTP HEAD request.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Head(rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.HEAD).URL(rawURL).WithOptions(ofs...).Do()

	return res, err
}

// Post is a convenience method for performing an HTTP POST request with a
// body.
//
// The body accepts the types supported by the request package: a string, a
// []byte, an io.Reader, an io.ReadSeeker, a *bytes.Buffer, a
// func() (io.Reader, error), or a *ReusableReadCloser.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Post(rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.POST).URL(rawURL).Body(body).WithOptions(ofs...).Do()

	return res, err
}

// Query is a convenience method for performing an HTTP QUERY request with a
// body, as defined by RFC 10008. The query is carried in the request body.
//
// QUERY is safe and idempotent, so it is retried by default under
// [WithRetryIdempotent]. The body accepts the types supported by [Client.Post].
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Query(rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.QUERY).URL(rawURL).Body(body).WithOptions(ofs...).Do()

	return res, err
}

// Put is a convenience method for performing an HTTP PUT request with a body.
// See [Client.Post] for the accepted body types.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Put(rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.PUT).URL(rawURL).Body(body).WithOptions(ofs...).Do()

	return res, err
}

// Patch is a convenience method for performing an HTTP PATCH request with a
// body. See [Client.Post] for the accepted body types.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - body (any): the request payload; nil means no body.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Patch(rawURL string, body any, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.PATCH).URL(rawURL).Body(body).WithOptions(ofs...).Do()

	return res, err
}

// Delete is a convenience method for performing an HTTP DELETE request.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Delete(rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.DELETE).URL(rawURL).WithOptions(ofs...).Do()

	return res, err
}

// Options is a convenience method for performing an HTTP OPTIONS request.
//
// Parameters:
//   - rawURL (string): the target URL. It must carry a scheme or a host.
//   - ofs (...RequestOptionFunc): per-request overrides of the execution
//     knobs.
//
// Returns:
//   - res (*Response): the response received from the server.
//   - err (error): non-nil when the request fails.
func (c *Client) Options(rawURL string, ofs ...RequestOptionFunc) (res *Response, err error) {
	res, err = c.Request().Method(hqgohttpmethod.OPTIONS).URL(rawURL).WithOptions(ofs...).Do()

	return res, err
}
