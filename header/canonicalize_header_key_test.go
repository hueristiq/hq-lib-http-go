package header_test

import (
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
