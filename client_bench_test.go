package http_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
	hqgohttprequest "github.com/hueristiq/hq-lib-http-go/request"
	hqgohttpstatus "github.com/hueristiq/hq-lib-http-go/status"
)

// responseSink and errSink keep benchmark results alive so the compiler
// cannot optimize the measured calls away.
var (
	responseSink *hqgohttp.Response
	errSink      error
	boolSink     bool
)

// Benchmark_Client_Do measures the happy path of a request execution —
// builder-free construction, the retry loop, and the transport round trip —
// against a local server.
func Benchmark_Client_Do(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OK"))
	}))

	b.Cleanup(server.Close)

	client := hqgohttp.New()

	req, err := hqgohttprequest.New(b.Context(), hqgohttpmethod.GET.String(), server.URL, nil)
	if err != nil {
		b.Fatalf("Expected no error, got %v", err)
	}

	b.ReportAllocs()

	var (
		res *hqgohttp.Response
	)

	for b.Loop() {
		res, err = client.Do(req)
		if err != nil {
			b.Fatalf("Expected no error, got %v", err)
		}

		_, _ = io.Copy(io.Discard, res.Body)

		_ = res.Body.Close()
	}

	responseSink = res
	errSink = err
}

// Benchmark_Client_Get measures the convenience path: request construction,
// default header and User-Agent application, and the round trip.
func Benchmark_Client_Get(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OK"))
	}))

	b.Cleanup(server.Close)

	client := hqgohttp.New()

	b.ReportAllocs()

	var (
		res *hqgohttp.Response
		err error
	)

	for b.Loop() {
		res, err = client.Get(server.URL)
		if err != nil {
			b.Fatalf("Expected no error, got %v", err)
		}

		_, _ = io.Copy(io.Discard, res.Body)

		_ = res.Body.Close()
	}

	responseSink = res
	errSink = err
}

// Benchmark_RequestBuilder_Do measures the full fluent chain — URL
// resolution, parameter merging, header application, and the round trip.
func Benchmark_RequestBuilder_Do(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OK"))
	}))

	b.Cleanup(server.Close)

	client := hqgohttp.New()

	b.ReportAllocs()

	var (
		res *hqgohttp.Response
		err error
	)

	for b.Loop() {
		res, err = client.Request().
			Method(hqgohttpmethod.GET).
			URL(server.URL+"/bench").
			Param("q", "1").
			SetHeader("X-Bench", "true").
			Context(b.Context()).Do()
		if err != nil {
			b.Fatalf("Expected no error, got %v", err)
		}

		_, _ = io.Copy(io.Discard, res.Body)

		_ = res.Body.Close()
	}

	responseSink = res
	errSink = err
}

// Benchmark_DefaultRetryPolicy measures the retry decision on the hot path —
// a successful response, the common case once a request has succeeded.
func Benchmark_DefaultRetryPolicy(b *testing.B) {
	res := &http.Response{StatusCode: hqgohttpstatus.OK.Int()}

	ctx := b.Context()

	b.ReportAllocs()

	var (
		retry bool
		err   error
	)

	for b.Loop() {
		retry, err = hqgohttp.DefaultRetryPolicy(ctx, nil, res, nil, 1)
	}

	boolSink = retry
	errSink = err
}
