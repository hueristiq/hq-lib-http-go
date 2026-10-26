package method_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	hqgohttpmethod "github.com/hueristiq/hq-lib-http-go/method"
)

func TestMethod_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   hqgohttpmethod.Method
		expected string
	}{
		{"CONNECT", hqgohttpmethod.CONNECT, http.MethodConnect},
		{"DELETE", hqgohttpmethod.DELETE, http.MethodDelete},
		{"GET", hqgohttpmethod.GET, http.MethodGet},
		{"HEAD", hqgohttpmethod.HEAD, http.MethodHead},
		{"OPTIONS", hqgohttpmethod.OPTIONS, http.MethodOptions},
		{"PATCH", hqgohttpmethod.PATCH, http.MethodPatch},
		{"POST", hqgohttpmethod.POST, http.MethodPost},
		{"PUT", hqgohttpmethod.PUT, http.MethodPut},
		{"QUERY", hqgohttpmethod.QUERY, "QUERY"},
		{"TRACE", hqgohttpmethod.TRACE, http.MethodTrace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, tt.method.String())
		})
	}
}
