package header_test

import (
	"testing"

	hqgohttpheader "github.com/hueristiq/hq-lib-http-go/header"
)

var canonicalSink string

func BenchmarkCanonicalizeHeaderKey(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		canonicalSink = hqgohttpheader.CanonicalizeHeaderKey("x-forwarded-for")
	}
}
