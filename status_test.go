package httpclient_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	hqgohttp "github.com/hueristiq/hq-lib-http-go"
)

func TestStatus_Int(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 200, hqgohttp.StatusOK.Int())
	assert.Equal(t, 404, hqgohttp.StatusNotFound.Int())
	assert.Equal(t, 500, hqgohttp.StatusInternalServerError.Int())
}

func TestStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   hqgohttp.Status
		expected string
	}{
		{"Continue", hqgohttp.StatusContinue, http.StatusText(http.StatusContinue)},
		{"EarlyHints", hqgohttp.StatusEarlyHints, http.StatusText(http.StatusEarlyHints)},
		{"OK", hqgohttp.StatusOK, http.StatusText(http.StatusOK)},
		{"Created", hqgohttp.StatusCreated, http.StatusText(http.StatusCreated)},
		{"NoContent", hqgohttp.StatusNoContent, http.StatusText(http.StatusNoContent)},
		{"IMUsed", hqgohttp.StatusIMUsed, http.StatusText(http.StatusIMUsed)},
		{"MovedPermanently", hqgohttp.StatusMovedPermanently, http.StatusText(http.StatusMovedPermanently)},
		{"TemporaryRedirect", hqgohttp.StatusTemporaryRedirect, http.StatusText(http.StatusTemporaryRedirect)},
		{"BadRequest", hqgohttp.StatusBadRequest, http.StatusText(http.StatusBadRequest)},
		{"Unauthorized", hqgohttp.StatusUnauthorized, http.StatusText(http.StatusUnauthorized)},
		{"NotFound", hqgohttp.StatusNotFound, http.StatusText(http.StatusNotFound)},
		{"Teapot", hqgohttp.StatusTeapot, http.StatusText(http.StatusTeapot)},
		{"MisdirectedRequest", hqgohttp.StatusMisdirectedRequest, http.StatusText(http.StatusMisdirectedRequest)},
		{"TooEarly", hqgohttp.StatusTooEarly, http.StatusText(http.StatusTooEarly)},
		{"TooManyRequests", hqgohttp.StatusTooManyRequests, http.StatusText(http.StatusTooManyRequests)},
		{"UnavailableForLegalReasons", hqgohttp.StatusUnavailableForLegalReasons, http.StatusText(http.StatusUnavailableForLegalReasons)},
		{"InternalServerError", hqgohttp.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)},
		{"ServiceUnavailable", hqgohttp.StatusServiceUnavailable, http.StatusText(http.StatusServiceUnavailable)},
		{"NetworkAuthenticationRequired", hqgohttp.StatusNetworkAuthenticationRequired, http.StatusText(http.StatusNetworkAuthenticationRequired)},
		{"Unknown", hqgohttp.Status(599), "Unknown Status (599)"},
		{"Zero", hqgohttp.Status(0), "Unknown Status (0)"},
		{"Unused 306", hqgohttp.Status(306), "Unknown Status (306)"},
		{"Out of range", hqgohttp.Status(600), "Unknown Status (600)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}

func TestStatus_Classification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		status          hqgohttp.Status
		isInformational bool
		isSuccess       bool
		isRedirection   bool
		isClientError   bool
		isServerError   bool
		isError         bool
	}{
		{"100 Continue", hqgohttp.StatusContinue, true, false, false, false, false, false},
		{"199 boundary", hqgohttp.Status(199), true, false, false, false, false, false},
		{"200 OK", hqgohttp.StatusOK, false, true, false, false, false, false},
		{"299 boundary", hqgohttp.Status(299), false, true, false, false, false, false},
		{"302 Found", hqgohttp.StatusFound, false, false, true, false, false, false},
		{"399 boundary", hqgohttp.Status(399), false, false, true, false, false, false},
		{"404 Not Found", hqgohttp.StatusNotFound, false, false, false, true, false, true},
		{"425 Too Early", hqgohttp.StatusTooEarly, false, false, false, true, false, true},
		{"499 boundary", hqgohttp.Status(499), false, false, false, true, false, true},
		{"500 Internal Server Error", hqgohttp.StatusInternalServerError, false, false, false, false, true, true},
		{"599 boundary", hqgohttp.Status(599), false, false, false, false, true, true},
		{"0 boundary", hqgohttp.Status(0), false, false, false, false, false, false},
		{"600 boundary", hqgohttp.Status(600), false, false, false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.isInformational, tt.status.IsInformational(), "IsInformational mismatch")
			assert.Equal(t, tt.isSuccess, tt.status.IsSuccess(), "IsSuccess mismatch")
			assert.Equal(t, tt.isRedirection, tt.status.IsRedirection(), "IsRedirection mismatch")
			assert.Equal(t, tt.isClientError, tt.status.IsClientError(), "IsClientError mismatch")
			assert.Equal(t, tt.isServerError, tt.status.IsServerError(), "IsServerError mismatch")
			assert.Equal(t, tt.isError, tt.status.IsError(), "IsError mismatch")
		})
	}
}
