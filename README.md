# hq-lib-http-go

![made with go](https://img.shields.io/badge/made%20with-Go-1E90FF.svg) [![go reference](https://pkg.go.dev/badge/github.com/hueristiq/hq-lib-http-go.svg)](https://pkg.go.dev/github.com/hueristiq/hq-lib-http-go) [![license](https://img.shields.io/badge/license-MIT-gray.svg?color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/blob/main/LICENSE) ![maintenance](https://img.shields.io/badge/maintained%3F-yes-1E90FF.svg) [![open issues](https://img.shields.io/github/issues-raw/hueristiq/hq-lib-http-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/issues?q=is:issue+is:open) [![closed issues](https://img.shields.io/github/issues-closed-raw/hueristiq/hq-lib-http-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-http-go/issues?q=is:issue+is:closed) [![contribution](https://img.shields.io/badge/contributions-welcome-1E90FF.svg)](https://github.com/hueristiq/hq-lib-http-go/blob/main/CONTRIBUTING.md)

`hq-lib-http-go` is a [Go](https://golang.org/) package for resilient, configurable HTTP communication.

## Resources

- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
	- [Client](#client)
		- [Protocol Selection](#protocol-selection)
		- [URL Parser Hook](#url-parser-hook)
			- [URL Handling](#url-handling)
		- [Rate Limiting](#rate-limiting)
		- [Retrier Hook](#retrier-hook)
		- [Client Lifecycle](#client-lifecycle)
	- [Fluent Request Builder](#fluent-request-builder)
		- [Request Context](#request-context)
		- [Request Inputs](#request-inputs)
	- [Response Handling](#response-handling)
	- [Retry Configuration](#retry-configuration)
	- [Retry Errors](#retry-errors)
	- [Retry Exhaustion Handler](#retry-exhaustion-handler)
	- [Standard Client Adapter](#standard-client-adapter)
	- [Per-Attempt Timeout](#per-attempt-timeout)
	- [Transport and Redirects](#transport-and-redirects)
	- [Dial and TLS Controls](#dial-and-tls-controls)
- [Configuration](#configuration)
- [Contributing](#contributing)
- [Licensing](#licensing)

## Features

- **HTTP/1.1, HTTP/2, and HTTP/3:** `WithProtocol` selects auto negotiation (the default), HTTP/3-first with fallback, or a forced version. HTTP/3 uses quic-go, requires HTTPS over UDP, and does not honor proxies: the auto mode uses HTTP/1.1+HTTP/2 when a proxy applies, and forced HTTP/3 returns `ErrHTTP3ProxyUnsupported`. HTTP/2 is supported over TLS and as cleartext h2c.
- **Fluent Request Builder:** Assemble requests with a chainable `Method().URL().Param().SetHeader().AddHeader().Body().Context(ctx).Do()` API. `WithContext` sets a client-level default; the builder `Context` overrides it per request, including an explicit `context.Background` or nil.
- **Client Lifecycle:** `Close` cancels in-flight requests, closes idle connections, and rejects later executions with `ErrClientClosed`; `CloseIdleConnections` releases pooled connections without disabling the client. The resolved configuration is private and immutable after `New`; `Config()` returns a snapshot.
- **Typed Request Inputs and Response Helpers:** `JSON`, `XML`, `Form`, and `Multipart` encode bodies and set the matching `Content-Type`; `BodyStream` and `MultipartForm.Stream` send large bodies without buffering them. `Do` returns a `Response` whose `Body` exposes `Bytes`, `String`, `JSON`, `XML`, `UTF8`, `Stream`, `SSE`, and `Close`, while `Response.Std()` keeps the raw response accessible. `WithMaxResponseBodySize` and `WithMaxRequestBodySize` cap how much the buffering helpers retain; raw readers and multipart are capped while reading/writing.
- **Retrier Hook:** `WithRetrier` accepts any engine with a `Retry` method — for example [hq-lib-retrier-go](https://github.com/hueristiq/hq-lib-retrier-go) adapted with `RetrierFunc` — without imposing a module dependency. The engine owns the attempt count, waits, and backoff; http-go supplies the retryable-error predicate, the retry event, and the parsed `Retry-After` wait. With no retrier, every execution makes a single attempt. See [Retrier Hook](#retrier-hook).
- **Idempotent-by-Default Retries:** Every entry point honors the caller's context. The retry policy retries connection and transport errors, `408`, `429`, and `5xx` responses, skips permanent failures such as NXDOMAIN, and gives the policy the attempt request, response, error, and attempt number. Only GET, HEAD, OPTIONS, TRACE, PUT, DELETE, and QUERY are retried by default; `WithRetryIdempotent(false)` opts POST and PATCH in. `WithOnRetry` reports the computed wait and attempt duration, `WithPrepareRetry` re-signs before the next attempt, and `WithAttemptTimeout` stops one slow attempt from consuming the whole budget.
- **Typed Retry Errors:** When retries run out, `Do` returns a `*RetryError` with the attempt count, attempt history, status, headers, and the last failure. A bounded body snapshot is captured only with `WithRetryErrorBodyLimit`; a snapshot read error is reported in `RetryError.BodyErr`. It matches `ErrRetriesExhausted` with `errors.Is` when the retry budget was exhausted, and `ErrBodyNotReplayable` when a body could not be replayed.
- **URL-Parser Hook:** `WithURLParser` accepts any parser with `Parse(raw) (*url.URL, error)` — for example [hq-lib-url-go](https://github.com/hueristiq/hq-lib-url-go)'s parser adapted with `request.URLParserFunc` — without imposing a module dependency. The default is `net/url.Parse`; a custom parser can add a default scheme, repair invalid percent escapes, or transform input. `WithRequestURLParser` overrides it per request. See [URL Parser Hook](#url-parser-hook).
- **Retry Exhaustion Handler:** `WithOnRetryExhaustion` can return the final response — body open — instead of the `*RetryError`, so a `5xx` that is data rather than a failure stays readable.
- **Transport and Dial Control:** `WithTransport`, `WithTransportBuilder`, `WithHTTP3TransportBuilder`, `WithDialContext`/`WithDialer`, `WithTLSHandshakeTimeout`, `WithTransportWrapper`, `WithProxy`, `WithCheckRedirect`, and `WithCookieJar` cover custom transports, dialers, middleware, proxies, redirect policy, and cookies without replacing the whole client. `WithHTTP3FallbackTTL` controls how long an H3-disabled host skips HTTP/3.
- **Attempt-Aware Callbacks:** `WithOnRequest` and `WithOnResponse` receive event structs with the attempt number; the response event also carries the attempt duration. Both have per-request overrides
- **Rate-Limiter Hook:** `WithRateLimiter` accepts any limiter with a `Wait(ctx) error` method — for example [hq-lib-limiter-go](https://github.com/hueristiq/hq-lib-limiter-go) — without imposing a module dependency. `RateLimiterFunc` adapts a plain function. See [Rate Limiting](#rate-limiting).

## Installation

To install `hq-lib-http-go`, run the following command in your Go project:

```bash
go get github.com/hueristiq/hq-lib-http-go@latest
```

Requires Go 1.26.3 or later.

## Usage

The examples below import the package under the `hqgohttp` alias.

```go
import hqgohttp "github.com/hueristiq/hq-lib-http-go"
```

The snippets below are illustrative.

### Client

`New` takes functional options. Set a client-level context with `WithContext`, or a per-request context with `RequestBuilder.Context`; the convenience methods take no context argument:

```go
package main

import (
	"fmt"
	"log"
	"time"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func main() {
	client := hqgohttp.New(
		hqgohttp.WithTimeout(10*time.Second),
	)

	response, err := client.Get("https://example.com")
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}

	defer response.Body.Close()

	fmt.Println("Response status:", response.Status)
}
```

No execution timeout is applied by default. The example sets one with `WithTimeout`; without it, bound the call with a context or `WithTimeout(RecommendedTimeout)`.

#### Protocol Selection

`WithProtocol` chooses the HTTP version. The default `ProtocolAuto` negotiates HTTP/2 over TLS and falls back to HTTP/1.1. `ProtocolAutoHTTP3` tries HTTP/3 first and falls back to auto; `ProtocolHTTP1`, `ProtocolHTTP2`, and `ProtocolHTTP3` force one version.

```go
client := hqgohttp.New(
	hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3),
	hqgohttp.WithTLSConfig(&tls.Config{RootCAs: roots}),
)
```

HTTP/3 requires `https://`, uses UDP, and does not honor HTTP proxies. `ProtocolAutoHTTP3` resolves the proxy per request and uses the HTTP/1.1+HTTP/2 client when one applies; `ProtocolHTTP3` returns `ErrHTTP3ProxyUnsupported` for such a request instead of bypassing the proxy. The auto fallback is used only for transient transport errors: a response paired with an error (a redirect-policy abort) is final, and permanent failures such as a certificate error or NXDOMAIN are not retried on another protocol. A host that fails HTTP/3 is sent through auto for `WithHTTP3FallbackTTL` (one minute by default). A `WithProxy` selector that returns nil disables environment proxying and allows HTTP/3. `WithTLSConfig` is cloned and applies only to transports the library builds; `WithHTTPClient` and `WithTransport` take precedence, so a custom client or transport configures its own TLS. `WithTransportWrapper` decorates the protocol-built transport and preserves version negotiation; `WithHTTP3TransportBuilder` customizes the HTTP/3 transport itself.

#### URL Parser Hook

`WithURLParser` accepts any value with a `Parse(raw string) (*url.URL, error)` method, so the client does not depend on a particular URL parser. The default is `net/url.Parse`, which is strict: it applies no default scheme and repairs no invalid percent escapes. A parser from [hq-lib-url-go](https://github.com/hueristiq/hq-lib-url-go) restores the scheme-defaulting and lenient behavior:

```go
package main

import (
	"net/url"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
	hqgourlparser "github.com/hueristiq/hq-lib-url-go/parser"
)

func main() {
	urlParser := hqgourlparser.New(
		hqgourlparser.WithDefaultScheme("http"),
		hqgourlparser.WithLenient(),
	)

	client := hqgohttp.New(
		hqgohttp.WithURLParser(hqgohttprequest.URLParserFunc(func(raw string) (*url.URL, error) {
			parsed, err := urlParser.Parse(raw)
			if err != nil {
				return nil, err
			}

			return parsed.URL, nil
		})),
	)

	res, err := client.Get("example.com/users")
	if err != nil {
		panic(err)
	}

	defer res.Body.Close()
}
```

A per-request parser overrides the client one:

```go
res, err := client.Get("example.com/users",
	hqgohttp.WithRequestURLParser(perHostParser),
)
```

`WithRequestURLParser(nil)` is ignored and keeps the client configuration. A parser shared across requests, clients, or goroutines must be safe for concurrent use. For requests built directly with `request.New`, pass `request.WithURLParser(...)`. The packages do not import hq-lib-url-go; the dependency belongs to the application.

##### URL Handling

`RequestBuilder` parses the URL with `net/url.Parse` unless a parser is configured. With the default, a request URL must be absolute and correctly escaped: `https://example.com/users` and `https://scanme.sh/w%0d%2e/` are valid; `example.com/users` (no scheme) and `https://scanme.sh/%invalid` (invalid escape) fail. Valid escapes and raw query text are preserved. `Param` appends to the raw query without rewriting existing pairs, so signed query strings keep their bytes.

With an hq-lib-url-go adapter as above, scheme-less hosts default to `http` and invalid escapes are repaired: `example.com/users`, `127.0.0.1:8080/users`, and `https://scanme.sh/%invalid` (sent as `/%25invalid`) are accepted.

Byte-exact malformed requests remain out of scope: `net/http` cannot emit a raw invalid escape such as `%invalid`; a lenient parser repairs it to `%25invalid` before the request is built. Invalid authority characters, such as a space in the host, still fail.

#### Rate Limiting

`WithRateLimiter` accepts any value with a `Wait(ctx) error` method, so the client does not depend on a particular limiter. A `*limiter.Limiter` from [hq-lib-limiter-go](https://github.com/hueristiq/hq-lib-limiter-go) is the suggested implementation:

```go
package main

import (
	"fmt"
	"time"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

func main() {
	limiter := hqgolimiter.New(
		hqgolimiter.WithRate(5, time.Second),
		hqgolimiter.WithBurst(2),
	)

	client := hqgohttp.New(
		hqgohttp.WithRateLimiter(limiter),
		hqgohttp.WithRetrier(retrier),
	)

	response, err := client.Get("https://api.example.com/users")
	if err != nil {
		panic(err)
	}

	defer response.Body.Close()

	fmt.Println(response.Status)
}
```

The limiter waits once before every retry-engine attempt, not once per request: with an engine configured for three attempts, one execution may take up to three tokens. The internal HTTP/2 or HTTP/3 fallback resend belongs to the same attempt and does not wait again. A `Wait` error stops the execution before that attempt; when the context ended the wait, its cause is returned.

A per-request limiter overrides the client one, which is useful for pacing one host or endpoint differently:

```go
response, err := client.Get(url,
	hqgohttp.WithRequestRateLimiter(perHostLimiter),
)
```

Passing nil, or omitting the option, disables limiting for the client; `WithRequestRateLimiter(nil)` is ignored and keeps the client configuration. A limiter shared across requests, clients, or goroutines must be safe for concurrent use.

To plug in a limiter whose method set does not match `RateLimiter`, wrap the call in `RateLimiterFunc`:

```go
client := hqgohttp.New(
	hqgohttp.WithRateLimiter(hqgohttp.RateLimiterFunc(func(ctx context.Context) error {
		return myLimiter.Wait(ctx)
	})),
)
```

Any `Wait(context.Context) error` implementation works, including a custom token bucket. The package does not import hq-lib-limiter-go; the dependency belongs to the application.

### Retrier Hook

`WithRetrier` accepts any value with a `Retry` method, so the client does not depend on a particular retry engine. The engine owns the attempt count, waits, and backoff; http-go supplies the operation and three HTTP-specific hooks in `RetrierOptions`: `RetryOn` (whether an attempt error is retryable), `OnRetry` (the retry event, and the point at which the failed response body is released), and `RetryAfter` (the wait parsed from a `Retry-After` header).

An adapter around [hq-lib-retrier-go](https://github.com/hueristiq/hq-lib-retrier-go) looks like this:

```go
package main

import (
	"context"
	"net/http"
	"time"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgoretrier "github.com/hueristiq/hq-lib-retrier-go"
	hqgoretrierbackoff "github.com/hueristiq/hq-lib-retrier-go/backoff"
)

func main() {
	retrier := hqgohttp.RetrierFunc(func(ctx context.Context, operation hqgohttp.RetryOperation, options hqgohttp.RetrierOptions) (*http.Response, error) {
		return hqgoretrier.RetryWithData(ctx, operation,
			hqgoretrier.WithMaxAttempts(4), // engine-owned policy
			hqgoretrier.WithWaitMin(500*time.Millisecond),
			hqgoretrier.WithWaitMax(10*time.Second),
			hqgoretrier.WithBackoff(func(minDelay, maxDelay time.Duration) hqgoretrierbackoff.Backoff {
				base := hqgoretrierbackoff.ExponentialWithDecorrelatedJitter(minDelay, maxDelay)

				return func(attempt int) time.Duration {
					if wait, ok := options.RetryAfter(); ok {
						return min(wait, maxDelay) // honor Retry-After, capped
					}

					return base(attempt)
				}
			}),
			hqgoretrier.WithRetryOn(options.RetryOn),
			hqgoretrier.WithOnRetry(func(event hqgoretrier.RetryEvent) {
				options.OnRetry(hqgohttp.RetrierEvent{Attempt: event.Attempt, Err: event.Err, Wait: event.Wait})
			}),
		)
	})

	client := hqgohttp.New(hqgohttp.WithRetrier(retrier))
	_ = client
}
```

The engine must call `OnRetry` after approving a retry and before its wait: the client's wrapper drains and closes the failed response body there, so the connection can be reused while the retry waits. It must also call `RetryOn` on every failed attempt and return the last result and error when it stops. `RetryAfter` reports the parsed `Retry-After` value, or `ok == false` when the header is absent or invalid; the adapter above prefers it over the backoff and caps it at the engine's maximum wait. Reading `RetryAfter` has no side effects.

Configure the engine's attempts, waits, and backoff in the adapter; the client does not duplicate those knobs. A per-request engine overrides the client one, which is useful when one host needs a different policy:

```go
response, err := client.Get(url,
	hqgohttp.WithRequestRetrier(perHostRetrier),
)
```

`WithRequestRetrier(nil)` disables retries for that execution. Omitting the option keeps the client configuration. A retrier shared across requests, clients, or goroutines must be safe for concurrent use. With no retrier, every execution makes a single attempt, and a persistent `429` or `5xx` still returns a `*RetryError`. The package does not import hq-lib-retrier-go; the dependency belongs to the application.

#### Client Lifecycle

`Close` shuts the client down: it cancels in-flight executions with `ErrClientClosed`, closes idle connections, and makes later requests fail. It is idempotent and safe to call concurrently:

```go
client := hqgohttp.New()
defer client.Close()
```

`CloseIdleConnections` is the non-destructive alternative; it frees pooled connections and leaves the client usable:

```go
client.CloseIdleConnections()
```

### Fluent Request Builder

`Request` starts a chainable builder; the client executes the built request with its retrier, rate limiter, and callbacks:

```go
package main

import (
	"fmt"
	"io"
	"log"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	"github.com/hueristiq/hq-lib-http-go/header"
	"github.com/hueristiq/hq-lib-http-go/method"
	"github.com/hueristiq/hq-lib-http-go/mime"
)

func main() {
	client := hqgohttp.New()

	response, err := client.Request().
		Method(method.POST).
		URL("https://api.example.com/users").
		Param("page", "1").
		SetHeader(header.ContentType.String(), mime.JSON.String()).
		AddHeader(header.Accept.String(), mime.JSON.String()).
		Body(`{"name":"ada"}`).
		Do()
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}

	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatalf("Reading response failed: %v", err)
	}

	fmt.Println("Response status:", response.Status)
	fmt.Println("Response body:", string(body))
}
```

#### Request Context

`WithContext` sets a client-level default context. It applies when a request carries no explicit context:

```go
client := hqgohttp.New(hqgohttp.WithContext(ctx))

res, err := client.Get("https://api.example.com/users")
```

Set the context per request with the builder `Context`, which overrides the client default. An explicit `context.Background()` or `Context(nil)` also overrides it:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

res, err := client.Request().
	URL("https://api.example.com/users").
	Context(ctx).
	Do()
```

A request created with `request.New(ctx, ...)` overrides the client default whenever `ctx` is non-nil, including an explicit `context.Background()`; `request.New(nil, ...)` leaves the client default in charge. A raw `http.Request` carrying a non-`context.Background` context also overrides the default, but an explicit `context.Background` on a raw request is indistinguishable from no context, so the default applies. `Do` takes no context argument. Package-level shortcuts still take an explicit `ctx` because they run against the shared default client.

#### Request Inputs

`JSON`, `XML`, `Form`, and `Multipart` encode the body when the request is built and set `Content-Type` only when the final request does not already carry one. A request header or client default wins. The body setters are mutually exclusive; the last call wins, and `Body` still accepts raw strings, bytes, and readers. A reader passed to `Body` is buffered when the setter is called, so repeated `Do` calls send the same payload; a reader implementing `io.Closer` is closed after buffering, and `WithMaxRequestBodySize` bounds the read. An empty buffered body is sent with `Content-Length: 0` instead of chunked encoding.

```go
res, err := client.Request().
	Method(method.POST).
	URL("https://api.example.com/users").
	JSON(map[string]any{"name": "ada"}).
	Do()
```

```go
values := url.Values{"tag": {"go", "http"}}

res, err := client.Request().
	Method(method.POST).
	URL("https://api.example.com/search").
	Form(values).
	Do()
```

```go
form := hqgohttp.NewMultipartForm().
	Field("description", "avatar").
	File("avatar", "avatar.png", file)

res, err := client.Request().
	Method(method.POST).
	URL("https://api.example.com/users/ada/avatar").
	Multipart(form).
	Do()
```

Multipart files are read and buffered when the request is built, so retries resend the complete body. File readers implementing `io.Closer` are closed after encoding. A form is single-use after it is built. `WithMaxRequestBodySize` caps the buffered size while the form is written.

For uploads too large to buffer, stream a raw body or a multipart form. A streamed body has no replay function, so a retry approved after the first attempt stops with `ErrBodyNotReplayable`:

```go
res, err := client.Request().
	Method(method.POST).
	URL("https://api.example.com/upload").
	BodyStream(file, fileSize). // -1 when the size is unknown
	SetHeader(header.ContentType.String(), "application/octet-stream").
	Do()
```

```go
body, contentType, err := form.Stream()
if err != nil {
	log.Fatalf("Encoding the form failed: %v", err)
}

defer body.Close()

res, err := client.Request().
	Method(method.POST).
	URL("https://api.example.com/users/ada/avatar").
	BodyStream(body, -1).
	SetHeader(header.ContentType.String(), contentType).
	Do()
```

### Response Handling

`Do` returns `*hqgohttp.Response`, which has a `Body *Body` and embeds `*http.Response`. The embedded fields keep working: `res.StatusCode`, `res.Header`, `res.Request`, and `res.TLS`. The raw body remains at `res.Std().Body`; `res.Body` is the enhanced wrapper, so do not read both.

```go
res, err := client.Get("https://api.example.com/users/ada")
if err != nil {
	log.Fatalf("Request failed: %v", err)
}

defer res.Body.Close()

if !res.IsSuccess() {
	log.Fatalf("Unexpected status: %s", res.Status)
}

var user User
if err := res.Body.JSON(&user); err != nil {
	log.Fatalf("Decoding the response failed: %v", err)
}
```

`res.Body.Bytes`, `String`, `JSON`, `XML`, and `UTF8` read and close the body on first use, then cache it, so later calls reuse the same content. An empty body yields `""` from `UTF8`. `Stream` returns a buffered `*StreamReader` for streaming with `Close` support, and `SSE` dispatches Server-Sent Events; the last event ID and retry time persist across events:

```go
stream := res.Body.Stream()
if stream != nil {
	defer stream.Close()
}
```

```go
err := res.Body.SSE(func(event *hqgohttp.SSEEvent) bool {
	fmt.Println(event.Event, event.Data)

	return true
})
```

`IsInformational`, `IsSuccess`, `IsRedirection`, `IsClientError`, `IsServerError`, and `IsError` classify the status. A non-retryable 4xx response is returned with a nil error; a persistent `429` or `5xx` exhausts retries into a `*RetryError` unless `WithOnRetryExhaustion` returns the final response. With no retrier, the single attempt still classifies that way. `res.Std()` returns the raw `*http.Response`; `Client.Std()` and `RoundTrip` continue to expose the standard-library types. For a request built with `request.New`, `Std()` returns the underlying `*http.Request`.

When `WithMaxResponseBodySize` is configured, the caching helpers stop at the cap and return the prefix read so far with an error matching `ErrResponseBodyTooLarge`. `Body.Stream` is not capped. `Body.Read` consumes the live body, so a later `Bytes` caches only the unread remainder.


### Retry Configuration

The retry policy decides, after each attempt, whether the outcome is worth retrying; the retrier decides how many attempts to make and how long to wait. Register an engine with `WithRetrier` (see [Retrier Hook](#retrier-hook)). The policy sees the attempt request, the response, the error, and the 1-based attempt number. The default policy retries connection and transport errors, `408`, `429`, and `5xx` (except `501`), and skips permanent failures such as NXDOMAIN; a custom policy can react to anything. `WithPrepareRetry` runs after the retry wait and before the next attempt, which is the place to re-sign a request; returning an error stops the execution.

A custom policy, retry callback, and pre-retry hook look like this:

```go
package main

import (
	"context"
	"log"
	"net/http"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func main() {
	client := hqgohttp.New(
		hqgohttp.WithRetrier(retrier), // configured in Retrier Hook
		hqgohttp.WithOnRetry(func(event hqgohttp.RetryEvent) {
			log.Printf("attempt %d failed after %s: %v; retrying in %s",
				event.Attempt, event.Duration, event.Err, event.Wait)
		}),
		hqgohttp.WithPrepareRetry(func(_ context.Context, req *http.Request, _ int) error {
			req.Header.Set("Authorization", "Bearer token") // re-sign after the wait
			return nil
		}),
		hqgohttp.WithRetryPolicy(func(ctx context.Context, req *http.Request, resp *http.Response, err error, attempt int) (bool, error) {
			if err == nil && resp != nil && resp.StatusCode == http.StatusForbidden {
				return false, nil // retrying cannot fix a 403
			}

			return hqgohttp.DefaultRetryPolicy(ctx, req, resp, err, attempt)
		}),
	)

	response, err := client.Get("https://example.com",
		hqgohttp.WithRequestRetrier(perHostRetrier), // per-request engine
	)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}

	defer response.Body.Close()

	log.Println("Response status:", response.Status)
}
```

Retries are idempotent-only by default: GET, HEAD, OPTIONS, TRACE, PUT, DELETE, and QUERY are retried; POST and PATCH are not. QUERY is safe and idempotent per RFC 10008. Pass `WithRetryIdempotent(false)` (or `WithRequestRetryIdempotent(false)`) to retry every method, or supply a policy that inspects `req.Method`. The restriction also covers the internal HTTP/2 or HTTP/3 fallback resend.

With no retrier, every execution makes one attempt, and a persistent `429` or `5xx` still returns a `*RetryError`. To get the final response instead, configure `WithOnRetryExhaustion` to return it.

### Retry Errors

When retries run out, `Do` returns a `*RetryError`. It carries the attempt count, the per-attempt history, the last status and headers, and the last failure. A copy of the final response body is included only when capture is enabled with `WithRetryErrorBodyLimit`. Use `errors.As` to inspect it:

```go
response, err := client.Get("https://api.example.com/users",
	hqgohttp.WithRequestRetryErrorBodyLimit(hqgohttp.RecommendedRetryErrorBodyLimit),
)
if err != nil {
	var retryErr *hqgohttp.RetryError
	if errors.As(err, &retryErr) {
		for _, attempt := range retryErr.History {
			log.Printf("attempt %d: status=%d err=%v in %s",
				attempt.Attempt, attempt.StatusCode, attempt.Err, attempt.Duration)
		}

		log.Printf("gave up after %d attempts; status=%d body=%s",
			retryErr.Attempts, retryErr.StatusCode, retryErr.Body)
	}

	log.Fatalf("Request failed: %v", err)
}

defer response.Body.Close()
```

Body capture is off by default because response payloads can contain credentials or personal data. When enabled, the snapshot is limited by `WithRetryErrorBodyLimit`; `RecommendedRetryErrorBodyLimit` is 64 KiB. `BodyTruncated` reports whether the server sent more. The final response body is closed after the snapshot; read `RetryError.Body` instead of the response. `RetryError.Header` is always captured, and `RetryError.URL` preserves query parameters, so redact these fields before logging. `errors.Is(err, hqgohttp.ErrRetriesExhausted)` classifies the failure without inspecting the message. When a retry is approved for an idempotent request and its body cannot be replayed, the execution stops with a `*RetryError` built from the last response, without another attempt. With no retrier configured, a GET 5xx still yields a `*RetryError` after one attempt, while a POST 5xx returns its response because idempotency blocks the retry.

### Retry Exhaustion Handler

`WithOnRetryExhaustion` replaces the default `*RetryError` outcome. It receives the final response with its body open, the last failure, and the attempt count. Return the response with a nil error to hand it to the caller:

```go
client := hqgohttp.New(
	hqgohttp.WithRetrier(retrier),
	hqgohttp.WithOnRetryExhaustion(func(resp *http.Response, err error, attempts int) (*http.Response, error) {
		if resp != nil {
			return resp, nil // keep the final 5xx response and its body
		}

		return nil, err
	}),
)
```

Returning a nil response makes the client close any response body and return the handler's error; returning `(nil, nil)` falls back to the `*RetryError`. `WithRequestOnRetryExhaustion` overrides the handler for one request.



### Standard Client Adapter

`Std()` returns an `*http.Client` whose transport runs the same execution path, and `RoundTrip` implements `http.RoundTripper` directly. Both return a received response with a nil error, including a `5xx`, as the standard interfaces require:

```go
std := hqgohttp.New(hqgohttp.WithRetrier(retrier)).Std()

req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/users", http.NoBody)
if err != nil {
	panic(err)
}

res, err := std.Do(req) // retries, then returns the final response
```

The returned client stops at the first response (`http.ErrUseLastResponse`), so redirects remain governed by the hq client. The client also satisfies `http.RoundTripper`, which lets `oauth2.Transport` and similar wrappers use it directly.

### Per-Attempt Timeout

`WithTimeout` bounds the whole execution, including retries and waits; it is off by default, and `WithTimeout(0)` or a negative value disables it. `WithAttemptTimeout` bounds one attempt through receipt of the response headers, so a slow first attempt cannot consume the entire budget:

```go
client := hqgohttp.New(
	hqgohttp.WithTimeout(hqgohttp.RecommendedTimeout), // 30s, opt-in
	hqgohttp.WithAttemptTimeout(5*time.Second),
)
```

A timed-out attempt is retryable. The attempt bound stops at the headers; reading the returned body stays tied to the caller's context and `WithResponseBodyTimeout`. Use `WithRequestAttemptTimeout` to override or disable the bound for one request. Without a client timeout, a stalled request is bounded only by the caller's context.

### Transport and Redirects

`WithTransport` replaces the library-built transport. `WithTransportWrapper` decorates it, which keeps protocol selection, TLS, and proxy settings:

```go
proxyURL := &url.URL{Scheme: "http", Host: "proxy.example:8080"}

client := hqgohttp.New(
	hqgohttp.WithProxy(func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	}),
	hqgohttp.WithCheckRedirect(func(req *http.Request, _ []*http.Request) error {
		if req.URL.Host != "api.example.com" {
			return http.ErrUseLastResponse
		}

		return nil
	}),
	hqgohttp.WithCookieJar(jar),
	hqgohttp.WithTransportWrapper(func(base http.RoundTripper) http.RoundTripper {
		return myMiddleware(base)
	}),
)
```

Precedence is `WithHTTPClient`, then `WithTransport`, then the protocol-built transport with `WithTransportBuilder`, `WithHTTP3TransportBuilder`, `WithDialContext`/`WithDialer`, `WithTLSHandshakeTimeout`, `WithProxy`, and `WithTransportWrapper`. `WithTransport` bypasses `WithProtocol`, `WithTLSConfig`, and `WithProxy`. The HTTP/1.x and HTTP/2 builder and dial options apply only to those transports; HTTP/3 uses its own transport and the HTTP/3 builder. `WithCheckRedirect` and `WithCookieJar` also apply to a cloned `WithHTTPClient` client, and the fallback client inherits both.

### Dial and TLS Controls

`WithDialContext` (or `WithDialer`) sets the dial function for library-built HTTP/1.x and HTTP/2 transports, which covers custom resolvers, connect-to dialing, interface binding, and Unix sockets. `WithTLSHandshakeTimeout` bounds the handshake. `WithTransportBuilder` receives the selected protocol and the configured transport for anything else:

```go
client := hqgohttp.New(
	hqgohttp.WithDialContext(func(ctx context.Context, network, address string) (net.Conn, error) {
		return myDialer.DialContext(ctx, network, address)
	}),
	hqgohttp.WithTLSHandshakeTimeout(5*time.Second),
	hqgohttp.WithTransportBuilder(func(protocol hqgohttp.Protocol, transport *http.Transport) *http.Transport {
		transport.MaxIdleConnsPerHost = 16
		return transport
	}),
)
```

A builder that returns nil keeps the transport unchanged. HTTP/3 ignores the TCP dial, TLS-handshake, and HTTP/1.x/HTTP/2 builder options; use `WithHTTP3TransportBuilder` for its `Dial`, `QUICConfig`, additional settings, and header limits:

```go
client := hqgohttp.New(
	hqgohttp.WithProtocol(hqgohttp.ProtocolAutoHTTP3),
	hqgohttp.WithHTTP3FallbackTTL(time.Minute), // skip H3 for a failed host
	hqgohttp.WithHTTP3TransportBuilder(func(transport *http3.Transport) *http3.Transport {
		transport.MaxResponseHeaderBytes = 64 << 10
		return transport
	}),
)
```


## Configuration

### Client options

Behavior is set through functional options passed to `New`. Any option left unset falls back to its default, and invalid values are normalized to the defaults.

| Option | Description | Default |
| --- | --- | --- |
| `WithHTTPClient(client)` | Underlying `*http.Client`. Wins over the protocol and transport options. Cloned when redirect or cookie options are applied; the fallback always clones it. Nil is ignored. | protocol-built client |
| `WithProtocol(protocol)` | HTTP protocol mode: `ProtocolAuto` (default), `ProtocolAutoHTTP3`, `ProtocolHTTP1`, `ProtocolHTTP2`, or `ProtocolHTTP3`. Invalid values fall back to auto. Ignored with `WithHTTPClient` or `WithTransport`. | `ProtocolAuto` |
| `WithTLSConfig(config)` | TLS configuration for library-built transports, cloned before use. Ignored with `WithHTTPClient` or `WithTransport`. | system defaults |
| `WithTransport(transport)` | Custom `http.RoundTripper` replacing the built transport. Bypasses protocol, TLS, dial, builder, proxy, and wrapper options. | none |
| `WithTransportBuilder(fn)` | Customizes a library-built `*http.Transport`; receives the selected protocol and the configured transport. Returning nil keeps it. Ignored with `WithHTTPClient` or `WithTransport`; HTTP/3 is not passed. | none |
| `WithHTTP3TransportBuilder(fn)` | Customizes a library-built `*http3.Transport`, including `Dial`, `QUICConfig`, additional settings, and header limits. Returning nil keeps it. Ignored with `WithHTTPClient` or `WithTransport`. | none |
| `WithHTTP3FallbackTTL(d)` | How long a host that failed an HTTP/3 attempt skips HTTP/3 in `ProtocolAutoHTTP3`. `<= 0` disables the cache. Forced HTTP/3 still tries HTTP/3. | `DefaultHTTP3FallbackTTL` (`1m`) |
| `WithDialContext(fn)` / `WithDialer(d)` | Dial function for library-built HTTP/1.x and HTTP/2 transports. Covers custom resolvers, connect-to dialing, and Unix sockets. Ignored with a custom client or transport. | pooled dialer |
| `WithTLSHandshakeTimeout(d)` | TLS handshake bound for library-built HTTP/1.x and HTTP/2 transports. `<= 0` disables it. | `10s` |
| `WithTransportWrapper(fn)` | Decorates the built transport after the builder and proxy, preserving protocol, TLS, and proxy settings. Ignored with `WithHTTPClient` or `WithTransport`. | none |
| `WithProxy(fn)` | Proxy selector for built HTTP/1.x and HTTP/2 transports. A selector returning nil disables environment proxying for that request and allows HTTP/3; return nil unconditionally to disable it for every request. A non-nil result makes `ProtocolAutoHTTP3` use the auto client and `ProtocolHTTP3` return `ErrHTTP3ProxyUnsupported`. Ignored with a custom client or transport. | `http.ProxyFromEnvironment` |
| `WithCheckRedirect(fn)` | Redirect policy. Return `http.ErrUseLastResponse` to stop following. Applies to built clients and a cloned custom client; the fallback inherits it. | net/http default (10 redirects) |
| `WithCookieJar(jar)` | Cookie jar. A nil jar clears a custom client's jar. The fallback inherits it. | none |
| `WithHeader(key, value)` | Default header set on every request, including `Client.Do`. `SetHeader` replaces it; `AddHeader` appends to it. `Host` maps to `Request.Host`. | none |
| `WithContext(ctx)` | Client-level default context, applied only when no request context is set. An explicit `context.Background`, a `RequestBuilder.Context` value, or a non-Background request context overrides it. Nil leaves no default. | none |
| `WithTimeout(d)` | Bound on the execution through receipt of the response headers, including retries and waits. Does not bound reading the returned body. `> 0` sets it; `<= 0` disables it. | none (`RecommendedTimeout` is `30s`) |
| `WithAttemptTimeout(d)` | Bound on one attempt through receipt of the response headers. A timed-out attempt is retryable. `<= 0` disables it. | disabled |
| `WithURLParser(p)` | Any [URL parser](#url-parser-hook) (`Parse(raw) (*url.URL, error)`) used by `RequestBuilder`. Nil selects `net/url.Parse`, which is strict. | `net/url.Parse` |
| `WithResponseBodyTimeout(d)` | Separate bound on reading a handed-out response body. `<= 0` disables it; the caller's context still applies. | `DefaultResponseBodyTimeout` (none) |
| `WithMaxResponseBodySize(n)` | Cap on bytes buffered by `Body.Bytes`, `String`, `JSON`, `XML`, and `UTF8`. `<= 0` is unlimited. `Body.Stream`, `Response.Std().Body`, and `RoundTrip` are not capped. | unlimited |
| `WithMaxRequestBodySize(n)` | Cap on bytes buffered into a request body. Raw readers and multipart stop at the cap; JSON/XML are capped while encoding; form is checked after encoding. `<= 0` is unlimited. Streaming bodies with an unknown length are not capped. | unlimited |
| `WithRetrier(r)` | Any [Retrier](#retrier-hook) (`Retry(ctx, operation, options)`) running the execution's attempts. The engine owns the attempt count, waits, and backoff. Nil makes a single attempt; a persistent 429/5xx still yields a `*RetryError`. | none |
| `WithRetryPolicy(policy)` | `func(ctx, req, resp, err, attempt) (retry bool, rerr error)` deciding per attempt whether to retry. Nil selects the default. | `DefaultRetryPolicy` |
| `WithRetryIdempotent(enabled)` | Restrict retries to GET, HEAD, OPTIONS, TRACE, PUT, DELETE, and QUERY even when the policy approves another attempt. Pass false to retry POST and PATCH. | enabled |
| `WithOnRetryExhaustion(fn)` | Decides what `Do` returns when retries run out; can return the final response, body open. Nil returns a `*RetryError`. | none |
| `WithOnRetry(fn)` | Callback invoked with the attempt, request, response, error, attempt duration, and computed wait before each retry wait. | none |
| `WithPrepareRetry(fn)` | Hook invoked after the retry wait and before the next attempt, for re-signing. Returning an error stops the execution. | none |
| `WithRetryErrorBodyLimit(n)` | Maximum bytes copied from the final response body into a `RetryError`. `0` disables body capture; a negative value is normalized to 0. | `DefaultRetryErrorBodyLimit` (0) |
| `WithRateLimiter(l)` | Any [RateLimiter](#rate-limiting) (`Wait(ctx) error`) waited on before every retry-engine attempt. Nil disables limiting. | none |
| `WithOnRequest(fn)` | Callback invoked with a `RequestEvent` (attempt and request) before every retry-engine attempt. | none |
| `WithOnResponse(fn)` | Callback invoked with a `ResponseEvent` (attempt, request, response, error, duration) after every retry-engine attempt. | none |
| `WithMaxRequestsBeforeIdleClose(n)` | Close idle connections after n completed executions. `<= 0` disables. | disabled |
| `WithResponseReadLimit(n)` | Maximum bytes drained from a retried response's body so the connection can be reused. `<= 0` falls back to the default. | `DefaultResponseReadLimit` (`4096`) |

### Request options

A single execution overrides the execution knobs through `RequestOptionFunc` values passed to `Do`, the convenience methods, or `RequestBuilder.WithOptions`. The client configuration stays untouched; invalid values are ignored, keeping the client configuration.

| Option | Description |
| --- | --- |
| `WithRequestURLParser(p)` | Override the [URL parser](#url-parser-hook) for the request; nil keeps the client configuration. |
| `WithRequestRetrier(r)` | Override the retry engine for the request; nil disables retries for it. |
| `WithRequestRetryPolicy(policy)` | Override the retry policy. |
| `WithRequestRetryIdempotent(enabled)` | Override the idempotency restriction for the request. Pass false to retry POST/PATCH. |
| `WithRequestOnRequest(fn)` | Override the pre-attempt callback. |
| `WithRequestOnResponse(fn)` | Override the post-attempt callback. |
| `WithRequestOnRetry(fn)` | Override the retry hook for the request. |
| `WithRequestPrepareRetry(fn)` | Override the pre-retry hook for the request. |
| `WithRequestOnRetryExhaustion(fn)` | Override the retry-exhaustion handler for the request. |
| `WithRequestRateLimiter(l)` | Override the [rate limiter](#rate-limiting). Nil keeps the client configuration. |
| `WithRequestAttemptTimeout(d)` | Override the per-attempt timeout; `<= 0` disables it for the request. |
| `WithRequestMaxResponseBodySize(n)` | Override the response-body cap; `0` removes it for the request, negative keeps the client configuration. |
| `WithRequestResponseReadLimit(n)` | Override the drain bound. |
| `WithRequestRetryErrorBodyLimit(n)` | Override the retry-error snapshot bound; `0` disables body capture for the request. |

No execution timeout is applied by default. `WithTimeout` adds one; a per-request timeout needs no option: derive it with `context.WithTimeout` on the context handed to the entry point. That context also bounds body reads. `WithAttemptTimeout` (or `WithRequestAttemptTimeout`) adds a per-attempt bound that stops at the response headers, and `WithResponseBodyTimeout` adds a separate body-read bound without shortening the execution timeout.

The `With*` helpers cover common settings; because `ClientOptionFunc` and `RequestOptionFunc` receive the exported `ClientOptions` and `RequestOptions`, callers can also write custom options. Nil options are ignored. After construction the client configuration is private and immutable; inspect a snapshot with `client.Config()`. Mutating the snapshot does not affect the client.

## Contributing

Contributions are welcome and encouraged! Feel free to submit [Pull Requests](https://github.com/hueristiq/hq-lib-http-go/pulls) or report [Issues](https://github.com/hueristiq/hq-lib-http-go/issues). For more details, check out the [contribution guidelines](https://github.com/hueristiq/hq-lib-http-go/blob/main/CONTRIBUTING.md).

A big thank you to all the [contributors](https://github.com/hueristiq/hq-lib-http-go/graphs/contributors) for your ongoing support!

![contributors](https://contrib.rocks/image?repo=hueristiq/hq-lib-http-go&max=500)

## Licensing

This package is licensed under the [MIT license](https://opensource.org/license/mit). You are free to use, modify, and distribute it, as long as you follow the terms of the license. You can find the full license text in the repository - [Full MIT license text](https://github.com/hueristiq/hq-lib-http-go/blob/main/LICENSE).
