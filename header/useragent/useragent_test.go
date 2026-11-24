package useragent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttpheaderuseragent "github.com/hueristiq/hq-lib-http-go/header/useragent"
)

func TestList(t *testing.T) {
	t.Parallel()

	userAgents := hqgohttpheaderuseragent.List()

	require.NotEmpty(t, userAgents)

	seen := make(map[string]struct{}, len(userAgents))

	for _, userAgent := range userAgents {
		assert.True(t, strings.HasPrefix(userAgent, "Mozilla/5.0"),
			"Expected %q to look like a browser User-Agent", userAgent)

		_, duplicate := seen[userAgent]

		assert.False(t, duplicate, "Expected %q to appear once", userAgent)

		seen[userAgent] = struct{}{}
	}
}

func TestList_ReturnsCopy(t *testing.T) {
	t.Parallel()

	userAgents := hqgohttpheaderuseragent.List()

	require.NotEmpty(t, userAgents)

	original := userAgents[0]
	userAgents[0] = "mutated"

	assert.Equal(t, original, hqgohttpheaderuseragent.List()[0],
		"Expected mutating the returned slice to leave the package catalog untouched")
}

func TestList_CatalogSize(t *testing.T) {
	t.Parallel()

	assert.Greater(t, len(hqgohttpheaderuseragent.List()), 1000,
		"Expected the generated catalog to cover many browser/platform/device combinations")
}

func TestList_FiltersCompose(t *testing.T) {
	t.Parallel()

	userAgents := hqgohttpheaderuseragent.List(
		hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
		hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserSafari),
	)

	require.NotEmpty(t, userAgents)

	for _, userAgent := range userAgents {
		assert.True(t, strings.Contains(userAgent, "iPhone") || strings.Contains(userAgent, "iPad"),
			"Expected only Safari on iOS values")
	}
}

func TestList_EmptyForUnsupportedCombination(t *testing.T) {
	t.Parallel()

	userAgents := hqgohttpheaderuseragent.List(
		hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop),
		hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserSamsungInternet),
	)

	assert.Empty(t, userAgents,
		"Expected Samsung Internet to have no desktop User-Agents")
}

func TestRandom(t *testing.T) {
	t.Parallel()

	allowed := make(map[string]struct{})

	for _, userAgent := range hqgohttpheaderuseragent.List() {
		allowed[userAgent] = struct{}{}
	}

	for range 100 {
		userAgent := hqgohttpheaderuseragent.Random()

		_, ok := allowed[userAgent]

		assert.True(t, ok, "Expected %q to come from the generated catalog", userAgent)
	}
}

func TestRandom_Filters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []hqgohttpheaderuseragent.OptionFunc
	}{
		{
			name:    "desktop",
			options: []hqgohttpheaderuseragent.OptionFunc{hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop)},
		},
		{
			name:    "mobile",
			options: []hqgohttpheaderuseragent.OptionFunc{hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile)},
		},
		{
			name:    "chrome",
			options: []hqgohttpheaderuseragent.OptionFunc{hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserChrome)},
		},
		{
			name: "safari mobile",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserSafari),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			allowed := make(map[string]struct{})

			for _, userAgent := range hqgohttpheaderuseragent.List(test.options...) {
				allowed[userAgent] = struct{}{}
			}

			require.NotEmpty(t, allowed)

			for range 50 {
				userAgent := hqgohttpheaderuseragent.Random(test.options...)

				_, ok := allowed[userAgent]

				assert.True(t, ok, "Expected %q to respect the filters", userAgent)
			}
		})
	}
}

func TestRandom_FallsBack(t *testing.T) {
	t.Parallel()

	desktop := make(map[string]struct{})

	for _, userAgent := range hqgohttpheaderuseragent.List(hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop)) {
		desktop[userAgent] = struct{}{}
	}

	for range 50 {
		userAgent := hqgohttpheaderuseragent.Random(
			hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop),
			hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserSamsungInternet),
		)

		_, ok := desktop[userAgent]

		assert.True(t, ok, "Expected the browser filter to be relaxed to a desktop value")
	}
}

func TestRandomDesktop(t *testing.T) {
	t.Parallel()

	allowed := make(map[string]struct{})

	for _, userAgent := range hqgohttpheaderuseragent.List(hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop)) {
		allowed[userAgent] = struct{}{}
	}

	for range 50 {
		userAgent := hqgohttpheaderuseragent.RandomDesktop()

		_, ok := allowed[userAgent]

		assert.True(t, ok, "Expected %q to be a desktop User-Agent", userAgent)
	}
}

func TestRandomMobile(t *testing.T) {
	t.Parallel()

	allowed := make(map[string]struct{})

	for _, userAgent := range hqgohttpheaderuseragent.List(hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile)) {
		allowed[userAgent] = struct{}{}
	}

	for range 50 {
		userAgent := hqgohttpheaderuseragent.RandomMobile()

		_, ok := allowed[userAgent]

		assert.True(t, ok, "Expected %q to be a mobile User-Agent", userAgent)
	}
}

func TestPlatform_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "desktop", hqgohttpheaderuseragent.PlatformDesktop.String())
	assert.Equal(t, "mobile", hqgohttpheaderuseragent.PlatformMobile.String())
}

func TestBrowser_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "chrome", hqgohttpheaderuseragent.BrowserChrome.String())
	assert.Equal(t, "firefox", hqgohttpheaderuseragent.BrowserFirefox.String())
	assert.Equal(t, "safari", hqgohttpheaderuseragent.BrowserSafari.String())
	assert.Equal(t, "edge", hqgohttpheaderuseragent.BrowserEdge.String())
	assert.Equal(t, "opera", hqgohttpheaderuseragent.BrowserOpera.String())
	assert.Equal(t, "samsung-internet", hqgohttpheaderuseragent.BrowserSamsungInternet.String())
}

func TestList_IgnoresNilOption(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, hqgohttpheaderuseragent.List(nil))
}
