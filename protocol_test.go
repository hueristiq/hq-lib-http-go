package http_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestProtocolString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		protocol hqgohttp.Protocol
		want     string
	}{
		{protocol: hqgohttp.ProtocolAuto, want: "auto"},
		{protocol: hqgohttp.ProtocolAutoHTTP3, want: "auto-http3"},
		{protocol: hqgohttp.ProtocolHTTP1, want: "http/1.1"},
		{protocol: hqgohttp.ProtocolHTTP2, want: "http/2"},
		{protocol: hqgohttp.ProtocolHTTP3, want: "http/3"},
		{protocol: hqgohttp.Protocol(99), want: "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.protocol.String())
	}
}
