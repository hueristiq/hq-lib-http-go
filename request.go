package http

import (
	"net/http"
	"strings"
	"time"
)

// RequestOptions holds per-request execution overrides. Use the WithRequest
// options with [Client.Do] instead of constructing this type directly.
type RequestOptions struct {
	AttemptTimeout    time.Duration
	RetryMaxAttempts  int
	Logger            Logger
	RetryPolicy       RetryPolicy
	RateLimiter       RateLimiter
	OnRequest         OnRequestFunc
	OnResponse        OnResponseFunc
	OnRetry           OnRetryFunc
	PrepareRetry      PrepareRetryFunc
	IdempotentRetries bool
	ResponseReadLimit int64
}

func (opts *RequestOptions) normalize() {
	if opts.AttemptTimeout < 0 {
		opts.AttemptTimeout = 0
	}

	if opts.RetryPolicy == nil {
		opts.RetryPolicy = DefaultRetryPolicy
	}

	if opts.RetryMaxAttempts <= 0 {
		opts.RetryMaxAttempts = defaultRetryMaxAttempts
	}

	if opts.ResponseReadLimit <= 0 {
		opts.ResponseReadLimit = DefaultResponseReadLimit
	}
}

// RequestOptionFunc configures one request execution.
type RequestOptionFunc func(opts *RequestOptions)

// HeaderField is one default HTTP header field.
type HeaderField struct {
	Key   string
	Value string
}

func (c *Client) requestOptions(ofs ...RequestOptionFunc) (opts *RequestOptions) {
	opts = &RequestOptions{
		AttemptTimeout:    c.config.AttemptTimeout,
		RetryMaxAttempts:  c.config.RetryMaxAttempts,
		Logger:            c.config.Logger,
		RetryPolicy:       c.config.RetryPolicy,
		RateLimiter:       c.config.RateLimiter,
		OnRequest:         c.config.OnRequest,
		OnResponse:        c.config.OnResponse,
		OnRetry:           c.config.OnRetry,
		PrepareRetry:      c.config.PrepareRetry,
		IdempotentRetries: c.config.IdempotentRetries,
		ResponseReadLimit: c.config.ResponseReadLimit,
	}

	for _, of := range ofs {
		if of != nil {
			of(opts)
		}
	}

	opts.normalize()

	return opts
}

func isHostHeader(key string) (host bool) {
	return strings.EqualFold(key, "Host")
}

func applyDefaultHeaders(req *http.Request, headers []HeaderField) {
	if len(headers) == 0 {
		return
	}

	if req.Header == nil {
		req.Header = make(http.Header)
	}

	for _, header := range headers {
		if isHostHeader(header.Key) {
			implicitHost := req.URL != nil && req.Host == req.URL.Host
			if req.Host == "" || implicitHost {
				req.Host = header.Value
			}

			continue
		}

		if hasHeader(req.Header, header.Key) {
			continue
		}

		req.Header.Set(header.Key, header.Value)
	}
}

func hasHeader(header http.Header, key string) (found bool) {
	for existing := range header {
		if strings.EqualFold(existing, key) {
			return true
		}
	}

	return false
}
