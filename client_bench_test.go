package http_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

var (
	responseSink *http.Response
	errSink      error
	boolSink     bool
)

func BenchmarkClientDo(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OK"))
	}))
	b.Cleanup(server.Close)

	client := mustNew(b)

	req, err := http.NewRequestWithContext(b.Context(), http.MethodGet, server.URL, http.NoBody)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		responseSink, errSink = client.Do(req)
		if errSink != nil {
			b.Fatal(errSink)
		}

		_, _ = io.Copy(io.Discard, responseSink.Body)
		_ = responseSink.Body.Close()
	}
}

func BenchmarkDefaultRetryPolicy(b *testing.B) {
	res := &http.Response{StatusCode: http.StatusOK}
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		boolSink, errSink = hqgohttp.DefaultRetryPolicy(ctx, nil, res, nil, 1)
	}
}
