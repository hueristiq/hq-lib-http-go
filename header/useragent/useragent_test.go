package useragent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttpuseragent "github.com/hueristiq/hq-lib-http-go/header/useragent"
)

func TestList(t *testing.T) {
	t.Parallel()

	userAgents := hqgohttpuseragent.List()

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

	userAgents := hqgohttpuseragent.List()

	require.NotEmpty(t, userAgents)

	original := userAgents[0]
	userAgents[0] = "mutated"

	assert.Equal(t, original, hqgohttpuseragent.List()[0],
		"Expected mutating the returned slice to leave the package catalog untouched")
}

func TestList_CatalogSize(t *testing.T) {
	t.Parallel()

	assert.Greater(t, len(hqgohttpuseragent.List()), 1000,
		"Expected the generated catalog to cover many browser/platform/device combinations")
}

func TestList_FiltersCompose(t *testing.T) {
	t.Parallel()

	userAgents := hqgohttpuseragent.List(
		hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
		hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserSafari),
	)

	require.NotEmpty(t, userAgents)

	for _, userAgent := range userAgents {
		assert.True(t, strings.Contains(userAgent, "iPhone") || strings.Contains(userAgent, "iPad"),
			"Expected only Safari on iOS values")
	}
}

func TestList_EmptyForUnsupportedCombination(t *testing.T) {
	t.Parallel()

	userAgents := hqgohttpuseragent.List(
		hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop),
		hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserSamsungInternet),
	)

	assert.Empty(t, userAgents,
		"Expected Samsung Internet to have no desktop User-Agents")
}

func TestRandom(t *testing.T) {
	t.Parallel()

	allowed := make(map[string]struct{})

	for _, userAgent := range hqgohttpuseragent.List() {
		allowed[userAgent] = struct{}{}
	}

	for range 100 {
		userAgent := hqgohttpuseragent.Random()

		_, ok := allowed[userAgent]

		assert.True(t, ok, "Expected %q to come from the generated catalog", userAgent)
	}
}

func TestRandom_Filters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []hqgohttpuseragent.OptionFunc
	}{
		{
			name:    "desktop",
			options: []hqgohttpuseragent.OptionFunc{hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop)},
		},
		{
			name:    "mobile",
			options: []hqgohttpuseragent.OptionFunc{hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile)},
		},
		{
			name:    "chrome",
			options: []hqgohttpuseragent.OptionFunc{hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserChrome)},
		},
		{
			name: "safari mobile",
			options: []hqgohttpuseragent.OptionFunc{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserSafari),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			allowed := make(map[string]struct{})

			for _, userAgent := range hqgohttpuseragent.List(test.options...) {
				allowed[userAgent] = struct{}{}
			}

			require.NotEmpty(t, allowed)

			for range 50 {
				userAgent := hqgohttpuseragent.Random(test.options...)

				_, ok := allowed[userAgent]

				assert.True(t, ok, "Expected %q to respect the filters", userAgent)
			}
		})
	}
}

func TestRandom_FallsBack(t *testing.T) {
	t.Parallel()

	desktop := make(map[string]struct{})

	for _, userAgent := range hqgohttpuseragent.List(hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop)) {
		desktop[userAgent] = struct{}{}
	}

	for range 50 {
		userAgent := hqgohttpuseragent.Random(
			hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop),
			hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserSamsungInternet),
		)

		_, ok := desktop[userAgent]

		assert.True(t, ok, "Expected the browser filter to be relaxed to a desktop value")
	}
}

func TestRandomDesktop(t *testing.T) {
	t.Parallel()

	allowed := make(map[string]struct{})

	for _, userAgent := range hqgohttpuseragent.List(hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop)) {
		allowed[userAgent] = struct{}{}
	}

	for range 50 {
		userAgent := hqgohttpuseragent.RandomDesktop()

		_, ok := allowed[userAgent]

		assert.True(t, ok, "Expected %q to be a desktop User-Agent", userAgent)
	}
}

func TestRandomMobile(t *testing.T) {
	t.Parallel()

	allowed := make(map[string]struct{})

	for _, userAgent := range hqgohttpuseragent.List(hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile)) {
		allowed[userAgent] = struct{}{}
	}

	for range 50 {
		userAgent := hqgohttpuseragent.RandomMobile()

		_, ok := allowed[userAgent]

		assert.True(t, ok, "Expected %q to be a mobile User-Agent", userAgent)
	}
}

func TestPlatform_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "desktop", hqgohttpuseragent.PlatformDesktop.String())
	assert.Equal(t, "mobile", hqgohttpuseragent.PlatformMobile.String())
}

func TestBrowser_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "chrome", hqgohttpuseragent.BrowserChrome.String())
	assert.Equal(t, "firefox", hqgohttpuseragent.BrowserFirefox.String())
	assert.Equal(t, "safari", hqgohttpuseragent.BrowserSafari.String())
	assert.Equal(t, "edge", hqgohttpuseragent.BrowserEdge.String())
	assert.Equal(t, "opera", hqgohttpuseragent.BrowserOpera.String())
	assert.Equal(t, "samsung-internet", hqgohttpuseragent.BrowserSamsungInternet.String())
}

func TestList_IgnoresNilOption(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, hqgohttpuseragent.List(nil))
}
