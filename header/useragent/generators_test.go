package useragent_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttpuseragent "github.com/hueristiq/hq-lib-http-go/header/useragent"
)

func TestGenerators_Formats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		options    []hqgohttpuseragent.Option
		patterns   []string
		contains   []string
		notContain []string
	}{
		{
			name: "chrome desktop",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserChrome),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(.+\) AppleWebKit/537\.36 \(KHTML, like Gecko\) Chrome/\d+\.\d+\.\d+\.\d+ Safari/537\.36$`,
			},
			contains:   []string{"Chrome/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "chrome mobile",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserChrome),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(Linux; Android \d+; .+\) AppleWebKit/537\.36 \(KHTML, like Gecko\) Chrome/\d+\.\d+\.\d+\.\d+ Mobile Safari/537\.36$`,
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) CriOS/\d+\.\d+\.\d+\.\d+ Mobile/15E148 Safari/604\.1$`,
			},
		},
		{
			name: "firefox desktop",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserFirefox),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(.+; rv:\d+\.\d+\) Gecko/20100101 Firefox/\d+\.\d+$`,
			},
			contains:   []string{"Firefox/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "firefox mobile",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserFirefox),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(Android \d+; Mobile; rv:\d+\.\d+\) Gecko/\d+\.\d+ Firefox/\d+\.\d+$`,
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) FxiOS/\d+\.\d+ Mobile/15E148 Safari/605\.1\.15$`,
			},
		},
		{
			name: "safari desktop",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserSafari),
			},
			patterns: []string{
				`^Mozilla/5\.0 \(Macintosh; [^)]+\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) Version/\d+\.\d+ Safari/605\.1\.15$`,
			},
			contains:   []string{"Version/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "safari ios",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserSafari),
			},
			patterns: []string{
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) Version/\d+\.\d+ Mobile/15E148 Safari/604\.1$`,
			},
			contains: []string{"Version/"},
		},
		{
			name: "edge desktop",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserEdge),
			},
			patterns: []string{
				`Chrome/\d+\.0\.0\.0 Safari/537\.36 Edg/\d+\.0\.0\.0$`,
			},
			contains:   []string{"Edg/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "edge mobile",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserEdge),
			},
			patterns: []string{
				`Chrome/\d+\.0\.0\.0 Mobile Safari/537\.36 EdgA/\d+\.0\.0\.0$`,
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) EdgiOS/\d+\.0\.0\.0 Mobile/15E148 Safari/604\.1$`,
			},
		},
		{
			name: "opera desktop",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserOpera),
			},
			patterns: []string{
				`Chrome/\d+\.0\.0\.0 Safari/537\.36 OPR/\d+\.0\.0\.0$`,
			},
			contains:   []string{"OPR/"},
			notContain: []string{"Mobile"},
		},
		{
			name: "opera mobile",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserOpera),
			},
			patterns: []string{
				`Chrome/\d+\.0\.0\.0 Mobile Safari/537\.36 OPR/\d+\.\d+\.\d+\.\d+$`,
				`^Mozilla/5\.0 \((iPhone|iPad).*; CPU (iPhone )?OS \d+_\d+ like Mac OS X\) AppleWebKit/605\.1\.15 \(KHTML, like Gecko\) OPiOS/6\.7\.3 Mobile/15E148 Safari/604\.1$`,
			},
		},
		{
			name: "samsung internet android",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserSamsungInternet),
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

			userAgents := hqgohttpuseragent.List(test.options...)

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

	browsers := []hqgohttpuseragent.Browser{
		hqgohttpuseragent.BrowserChrome,
		hqgohttpuseragent.BrowserFirefox,
		hqgohttpuseragent.BrowserSafari,
		hqgohttpuseragent.BrowserEdge,
		hqgohttpuseragent.BrowserOpera,
		hqgohttpuseragent.BrowserSamsungInternet,
	}
	platforms := []hqgohttpuseragent.Platform{
		hqgohttpuseragent.PlatformDesktop,
		hqgohttpuseragent.PlatformMobile,
	}

	for _, browser := range browsers {
		for _, platform := range platforms {
			name := browser.String() + "/" + platform.String()

			t.Run(name, func(t *testing.T) {
				t.Parallel()

				userAgents := hqgohttpuseragent.List(
					hqgohttpuseragent.WithBrowser(browser),
					hqgohttpuseragent.WithPlatform(platform),
				)

				if browser == hqgohttpuseragent.BrowserSamsungInternet && platform == hqgohttpuseragent.PlatformDesktop {
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
		options []hqgohttpuseragent.Option
		pattern string
	}{
		{
			name: "edge desktop",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformDesktop),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserEdge),
			},
			pattern: `Chrome/(\d+)\.0\.0\.0 Safari/537\.36 Edg/(\d+)\.0\.0\.0`,
		},
		{
			name: "edge android",
			options: []hqgohttpuseragent.Option{
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
				hqgohttpuseragent.WithBrowser(hqgohttpuseragent.BrowserEdge),
			},
			pattern: `Chrome/(\d+)\.0\.0\.0 Mobile Safari/537\.36 EdgA/(\d+)\.0\.0\.0`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pattern := regexp.MustCompile(test.pattern)

			for _, userAgent := range hqgohttpuseragent.List(test.options...) {
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

	browsers := []hqgohttpuseragent.Browser{
		hqgohttpuseragent.BrowserChrome,
		hqgohttpuseragent.BrowserFirefox,
		hqgohttpuseragent.BrowserSafari,
		hqgohttpuseragent.BrowserEdge,
		hqgohttpuseragent.BrowserOpera,
		hqgohttpuseragent.BrowserSamsungInternet,
	}

	for _, browser := range browsers {
		t.Run(browser.String(), func(t *testing.T) {
			t.Parallel()

			for _, userAgent := range hqgohttpuseragent.List(
				hqgohttpuseragent.WithBrowser(browser),
				hqgohttpuseragent.WithPlatform(hqgohttpuseragent.PlatformMobile),
			) {
				assert.True(t, strings.Contains(userAgent, "Mobile") || strings.Contains(userAgent, "Android"),
					"Expected %q to advertise a mobile platform", userAgent)
			}
		})
	}
}
