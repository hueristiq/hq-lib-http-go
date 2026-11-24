package httpclient_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestMethod_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   hqgohttp.Method
		expected string
	}{
		{"CONNECT", hqgohttp.MethodConnect, http.MethodConnect},
		{"DELETE", hqgohttp.MethodDelete, http.MethodDelete},
		{"GET", hqgohttp.MethodGet, http.MethodGet},
		{"HEAD", hqgohttp.MethodHead, http.MethodHead},
		{"OPTIONS", hqgohttp.MethodOptions, http.MethodOptions},
		{"PATCH", hqgohttp.MethodPatch, http.MethodPatch},
		{"POST", hqgohttp.MethodPost, http.MethodPost},
		{"PUT", hqgohttp.MethodPut, http.MethodPut},
		{"QUERY", hqgohttp.MethodQuery, "QUERY"},
		{"TRACE", hqgohttp.MethodTrace, http.MethodTrace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, tt.method.String())
		})
	}
}
