package useragent_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttpheaderuseragent "github.com/hueristiq/hq-lib-http-go/header/useragent"
)

func TestGenerators_Formats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		options    []hqgohttpheaderuseragent.OptionFunc
		patterns   []string
		contains   []string
		notContain []string
	}{
		{
			name: "chrome desktop",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserChrome),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(.+\) AppleWebKit/537\.36 \(KHTML, like Gecko\) Chrome/\d+\.\d+\.\d+\.\d+ Safari/537\.36$`,
			},
			contains:   []string{"Chrome/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "chrome mobile",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserChrome),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(Linux; Android \d+; .+\) AppleWebKit/537\.36 \(KHTML, like Gecko\) Chrome/\d+\.\d+\.\d+\.\d+ Mobile Safari/537\.36$`,
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) CriOS/\d+\.\d+\.\d+\.\d+ Mobile/15E148 Safari/604\.1$`,
			},
		},
		{
			name: "firefox desktop",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserFirefox),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(.+; rv:\d+\.\d+\) Gecko/20100101 Firefox/\d+\.\d+$`,
			},
			contains:   []string{"Firefox/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "firefox mobile",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserFirefox),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(Android \d+; Mobile; rv:\d+\.\d+\) Gecko/\d+\.\d+ Firefox/\d+\.\d+$`,
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) FxiOS/\d+\.\d+ Mobile/15E148 Safari/605\.1\.15$`,
			},
		},
		{
			name: "safari desktop",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserSafari),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(Macintosh; [^)]+\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) Version/\d+\.\d+ Safari/605\.1\.15$`,
			},
			contains:   []string{"Version/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "safari ios",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserSafari),
			},
			patterns: []string{
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) Version/\d+\.\d+ Mobile/15E148 Safari/604\.1$`,
			},
			contains: []string{"Version/"},
		},
		{
			name: "edge desktop",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserEdge),
			},
			patterns: []string{
				`Chrome/\d+\.0\.0\.0 Safari/537\.36 Edg/\d+\.0\.0\.0$`,
			},
			contains:   []string{"Edg/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "edge mobile",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserEdge),
			},
			patterns: []string{
				`Chrome/\d+\.0\.0\.0 Mobile Safari/537\.36 EdgA/\d+\.0\.0\.0$`,
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) EdgiOS/\d+\.0\.0\.0 Mobile/15E148 Safari/604\.1$`,
			},
		},
		{
			name: "opera desktop",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserOpera),
			},
			patterns: []string{
				`Chrome/\d+\.0\.0\.0 Safari/537\.36 OPR/\d+\.0\.0\.0$`,
			},
			contains:   []string{"OPR/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "opera mobile",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserOpera),
			},
			patterns: []string{
				`Chrome/\d+\.0\.0\.0 Mobile Safari/537\.36 OPR/\d+\.\d+\.\d+\.\d+$`,
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) OPiOS/6\.7\.3 Mobile/15E148 Safari/604\.1$`,
			},
		},
		{
			name: "samsung internet android",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserSamsungInternet),
			},
			patterns: []string{
				`SamsungBrowser/\d+\.\d+\.\d+\.\d+ Chrome/\d+\.0\.0\.0 Mobile Safari/537\.36$`,
			},
			contains: []string{"SamsungBrowser/"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			userAgents := hqgohttpheaderuseragent.List(test.options...)

			require.NotEmpty(t, userAgents)

			patterns := make([]*regexp.Regexp, 0, len(test.patterns))

			for _, pattern := range test.patterns {
				patterns = append(patterns, regexp.MustCompile(pattern))
			}

			for _, userAgent := range userAgents {
				matched := false

				for _, pattern := range patterns {
					if pattern.MatchString(userAgent) {
						matched = true

						break
					}
				}

				assert.True(t, matched, "Expected %q to match one of %v", userAgent, test.patterns)

				for _, token := range test.contains {
					assert.Contains(t, userAgent, token)
				}

				for _, token := range test.notContain {
					assert.NotContains(t, userAgent, token)
				}
			}
		})
	}
}

func TestGenerators_Coverage(t *testing.T) {
	t.Parallel()

	browsers := []hqgohttpheaderuseragent.Browser{
		hqgohttpheaderuseragent.BrowserChrome,
		hqgohttpheaderuseragent.BrowserFirefox,
		hqgohttpheaderuseragent.BrowserSafari,
		hqgohttpheaderuseragent.BrowserEdge,
		hqgohttpheaderuseragent.BrowserOpera,
		hqgohttpheaderuseragent.BrowserSamsungInternet,
	}
	platforms := []hqgohttpheaderuseragent.Platform{
		hqgohttpheaderuseragent.PlatformDesktop,
		hqgohttpheaderuseragent.PlatformMobile,
	}

	for _, browser := range browsers {
		for _, platform := range platforms {
			name := browser.String() + "/" + platform.String()

			t.Run(name, func(t *testing.T) {
				t.Parallel()

				userAgents := hqgohttpheaderuseragent.List(
					hqgohttpheaderuseragent.WithBrowser(browser),
					hqgohttpheaderuseragent.WithPlatform(platform),
				)

				if browser == hqgohttpheaderuseragent.BrowserSamsungInternet && platform == hqgohttpheaderuseragent.PlatformDesktop {
					assert.Empty(t, userAgents)

					return
				}

				assert.NotEmpty(t, userAgents)
			})
		}
	}
}

func TestGenerators_VendorVersionAlignment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []hqgohttpheaderuseragent.OptionFunc
		pattern string
	}{
		{
			name: "edge desktop",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformDesktop),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserEdge),
			},
			pattern: `Chrome/(\d+)\.0\.0\.0 Safari/537\.36 Edg/(\d+)\.0\.0\.0`,
		},
		{
			name: "edge android",
			options: []hqgohttpheaderuseragent.OptionFunc{
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
				hqgohttpheaderuseragent.WithBrowser(hqgohttpheaderuseragent.BrowserEdge),
			},
			pattern: `Chrome/(\d+)\.0\.0\.0 Mobile Safari/537\.36 EdgA/(\d+)\.0\.0\.0`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pattern := regexp.MustCompile(test.pattern)

			for _, userAgent := range hqgohttpheaderuseragent.List(test.options...) {
				if strings.Contains(userAgent, "EdgiOS/") {
					continue
				}

				matches := pattern.FindStringSubmatch(userAgent)

				require.Len(t, matches, 3, "Expected %q to match %s", userAgent, test.pattern)

				assert.Equal(t, matches[1], matches[2],
					"Expected the Chromium and Edge versions in %q to align", userAgent)
			}
		})
	}
}

func TestGenerators_MobileValuesContainMobileToken(t *testing.T) {
	t.Parallel()

	browsers := []hqgohttpheaderuseragent.Browser{
		hqgohttpheaderuseragent.BrowserChrome,
		hqgohttpheaderuseragent.BrowserFirefox,
		hqgohttpheaderuseragent.BrowserSafari,
		hqgohttpheaderuseragent.BrowserEdge,
		hqgohttpheaderuseragent.BrowserOpera,
		hqgohttpheaderuseragent.BrowserSamsungInternet,
	}

	for _, browser := range browsers {
		t.Run(browser.String(), func(t *testing.T) {
			t.Parallel()

			for _, userAgent := range hqgohttpheaderuseragent.List(
				hqgohttpheaderuseragent.WithBrowser(browser),
				hqgohttpheaderuseragent.WithPlatform(hqgohttpheaderuseragent.PlatformMobile),
			) {
				assert.True(t, strings.Contains(userAgent, "Mobile") || strings.Contains(userAgent, "Android"),
					"Expected %q to advertise a mobile platform", userAgent)
			}
		})
	}
}
