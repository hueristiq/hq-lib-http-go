package header_test

import (
	"net/textproto"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	hqgohttpheader "github.com/hueristiq/hq-lib-http-go/header"
)

func TestCanonicalizeHeaderKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "all lowercase single word",
			input:    "content-type",
			expected: "Content-Type",
		},
		{
			name:     "mixed case single word",
			input:    "CoNtEnT-TyPe",
			expected: "Content-Type",
		},
		{
			name:     "all uppercase single word",
			input:    "CONTENT-TYPE",
			expected: "Content-Type",
		},
		{
			name:     "multiple hyphens",
			input:    "x-forwarded-for",
			expected: "X-Forwarded-For",
		},
		{
			name:     "single character header",
			input:    "x",
			expected: "X",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "already canonicalized",
			input:    "User-Agent",
			expected: "User-Agent",
		},
		{
			name:     "no hyphens",
			input:    "etag",
			expected: "Etag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := hqgohttpheader.CanonicalizeHeaderKey(tt.input)

			assert.Equal(t, tt.expected, result, "CanonicalizeHeaderKey(%q) = %q; want %q", tt.input, result, tt.expected)
		})
	}
}

func TestCanonicalizeHeaderKey_MatchesTextproto(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"content-type",
		"ETag",
		"WWW-Authenticate",
		"X-UA-Compatible",
		"TE",
		"DNT",
		"Sec-WebSocket-Accept",
		"x-forwarded-for",
	} {
		result := hqgohttpheader.CanonicalizeHeaderKey(key)

		assert.Equal(t, textproto.CanonicalMIMEHeaderKey(key), result,
			"CanonicalizeHeaderKey(%q) must match net/textproto", key)
		assert.Equal(t, result, hqgohttpheader.CanonicalizeHeaderKey(result),
			"CanonicalizeHeaderKey must be idempotent for %q", key)
	}
}

// FuzzCanonicalizeHeaderKey exercises the canonicalizer over arbitrary
// inputs: no input may panic, and the result must be idempotent.
func FuzzCanonicalizeHeaderKey(f *testing.F) {
	f.Add("Content-Type")
	f.Add("x-forwarded-for")
	f.Add("")
	f.Add("Sec-WebSocket-Accept")

	f.Fuzz(func(t *testing.T, key string) {
		canonical := hqgohttpheader.CanonicalizeHeaderKey(key)

		if again := hqgohttpheader.CanonicalizeHeaderKey(canonical); again != canonical {
			t.Fatalf("CanonicalizeHeaderKey(%q) is not idempotent: %q", key, again)
		}

		if !strings.EqualFold(canonical, key) {
			t.Fatalf("CanonicalizeHeaderKey(%q) changed the key's case-insensitive value: %q", key, canonical)
		}
	})
}

var canonicalSink string

// BenchmarkCanonicalizeHeaderKey measures the header canonicalization hot
// path used on every header set.
func BenchmarkCanonicalizeHeaderKey(b *testing.B) {
	key := "x-forwarded-for"

	b.ReportAllocs()

	for range b.N {
		canonicalSink = hqgohttpheader.CanonicalizeHeaderKey(key)
	}
}
