package status_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	hqgohttpstatus "github.com/hueristiq/hq-lib-http-go/status"
)

func TestStatus_Int(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 200, hqgohttpstatus.OK.Int())
	assert.Equal(t, 404, hqgohttpstatus.NotFound.Int())
	assert.Equal(t, 500, hqgohttpstatus.InternalServerError.Int())
}

func TestStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   hqgohttpstatus.Status
		expected string
	}{
		{"Continue", hqgohttpstatus.Continue, http.StatusText(http.StatusContinue)},
		{"EarlyHints", hqgohttpstatus.EarlyHints, http.StatusText(http.StatusEarlyHints)},
		{"OK", hqgohttpstatus.OK, http.StatusText(http.StatusOK)},
		{"Created", hqgohttpstatus.Created, http.StatusText(http.StatusCreated)},
		{"NoContent", hqgohttpstatus.NoContent, http.StatusText(http.StatusNoContent)},
		{"IMUsed", hqgohttpstatus.IMUsed, http.StatusText(http.StatusIMUsed)},
		{"MovedPermanently", hqgohttpstatus.MovedPermanently, http.StatusText(http.StatusMovedPermanently)},
		{"TemporaryRedirect", hqgohttpstatus.TemporaryRedirect, http.StatusText(http.StatusTemporaryRedirect)},
		{"BadRequest", hqgohttpstatus.BadRequest, http.StatusText(http.StatusBadRequest)},
		{"Unauthorized", hqgohttpstatus.Unauthorized, http.StatusText(http.StatusUnauthorized)},
		{"NotFound", hqgohttpstatus.NotFound, http.StatusText(http.StatusNotFound)},
		{"Teapot", hqgohttpstatus.Teapot, http.StatusText(http.StatusTeapot)},
		{"MisdirectedRequest", hqgohttpstatus.MisdirectedRequest, http.StatusText(http.StatusMisdirectedRequest)},
		{"TooEarly", hqgohttpstatus.TooEarly, http.StatusText(http.StatusTooEarly)},
		{"TooManyRequests", hqgohttpstatus.TooManyRequests, http.StatusText(http.StatusTooManyRequests)},
		{"UnavailableForLegalReasons", hqgohttpstatus.UnavailableForLegalReasons, http.StatusText(http.StatusUnavailableForLegalReasons)},
		{"InternalServerError", hqgohttpstatus.InternalServerError, http.StatusText(http.StatusInternalServerError)},
		{"ServiceUnavailable", hqgohttpstatus.ServiceUnavailable, http.StatusText(http.StatusServiceUnavailable)},
		{"NetworkAuthenticationRequired", hqgohttpstatus.NetworkAuthenticationRequired, http.StatusText(http.StatusNetworkAuthenticationRequired)},
		{"Unknown", hqgohttpstatus.Status(599), "Unknown Status (599)"},
		{"Zero", hqgohttpstatus.Status(0), "Unknown Status (0)"},
		{"Unused 306", hqgohttpstatus.Status(306), "Unknown Status (306)"},
		{"Out of range", hqgohttpstatus.Status(600), "Unknown Status (600)"},
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
		status          hqgohttpstatus.Status
		isInformational bool
		isSuccess       bool
		isRedirection   bool
		isClientError   bool
		isServerError   bool
		isError         bool
	}{
		{"100 Continue", hqgohttpstatus.Continue, true, false, false, false, false, false},
		{"199 boundary", hqgohttpstatus.Status(199), true, false, false, false, false, false},
		{"200 OK", hqgohttpstatus.OK, false, true, false, false, false, false},
		{"299 boundary", hqgohttpstatus.Status(299), false, true, false, false, false, false},
		{"302 Found", hqgohttpstatus.Found, false, false, true, false, false, false},
		{"399 boundary", hqgohttpstatus.Status(399), false, false, true, false, false, false},
		{"404 Not Found", hqgohttpstatus.NotFound, false, false, false, true, false, true},
		{"425 Too Early", hqgohttpstatus.TooEarly, false, false, false, true, false, true},
		{"499 boundary", hqgohttpstatus.Status(499), false, false, false, true, false, true},
		{"500 Internal Server Error", hqgohttpstatus.InternalServerError, false, false, false, false, true, true},
		{"599 boundary", hqgohttpstatus.Status(599), false, false, false, false, true, true},
		{"0 boundary", hqgohttpstatus.Status(0), false, false, false, false, false, false},
		{"600 boundary", hqgohttpstatus.Status(600), false, false, false, false, false, false},
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
