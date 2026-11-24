package useragent

import (
	"math/rand/v2"
)

// entry is one User-Agent value together with the filters it answers to.
type entry struct {
	userAgent string
	platform  Platform
	browser   Browser
}

// Options holds the filters applied by [Random] and [List].
type Options struct {
	Platform Platform
	Browser  Browser
}

// OptionFunc configures a User-Agent draw or listing. The zero value of a filter
// means no constraint. A nil OptionFunc is ignored.
type OptionFunc func(*Options)

// filterKey identifies one combination of browser and platform filters.
type filterKey struct {
	browser  Browser
	platform Platform
}

var (
	// catalog is every User-Agent value the package can return. It is
	// generated once, when the package initializes, and never mutated
	// afterwards.
	catalog = buildCatalog()

	// catalogIndex groups the catalog by browser, platform, and both, so a
	// filtered draw or listing does not scan the whole catalog.
	catalogIndex = buildCatalogIndex(catalog)
)

// WithPlatform returns an [OptionFunc] that restricts the result to one platform.
func WithPlatform(platform Platform) (option OptionFunc) {
	option = func(opts *Options) {
		opts.Platform = platform
	}

	return option
}

// WithBrowser returns an [OptionFunc] that restricts the result to one browser.
func WithBrowser(browser Browser) (option OptionFunc) {
	option = func(opts *Options) {
		opts.Browser = browser
	}

	return option
}

// buildCatalog enumerates every generator and removes duplicate values.
func buildCatalog() (entries []entry) {
	entries = make([]entry, 0, 4096)

	seen := make(map[string]struct{})

	for _, current := range generators {
		for _, userAgent := range current.all() {
			if userAgent == "" {
				continue
			}

			if _, duplicate := seen[userAgent]; duplicate {
				continue
			}

			seen[userAgent] = struct{}{}

			entries = append(entries, entry{
				userAgent: userAgent,
				platform:  current.platform,
				browser:   current.browser,
			})
		}
	}

	return entries
}

// buildCatalogIndex groups entries by browser, platform, and their
// intersection.
func buildCatalogIndex(entries []entry) (index map[filterKey][]entry) {
	index = make(map[filterKey][]entry)

	for _, current := range entries {
		browserKey := filterKey{browser: current.browser}
		platformKey := filterKey{platform: current.platform}
		bothKey := filterKey{browser: current.browser, platform: current.platform}

		index[browserKey] = append(index[browserKey], current)
		index[platformKey] = append(index[platformKey], current)
		index[bothKey] = append(index[bothKey], current)
	}

	return index
}

// resolveOptions applies the functional options in order.
func resolveOptions(opts []OptionFunc) (resolved Options) {
	for _, option := range opts {
		if option == nil {
			continue
		}

		option(&resolved)
	}

	return resolved
}

// matchingEntries returns the catalog entries that satisfy the filters. The
// returned slice is shared with the package index and must not be mutated.
func matchingEntries(filter Options) (entries []entry) {
	if filter.Platform == "" && filter.Browser == "" {
		return catalog
	}

	entries = catalogIndex[filterKey{browser: filter.Browser, platform: filter.Platform}]

	return entries
}

// List returns every User-Agent value that matches the given filters.
//
// With no options it returns the entire generated catalog. Filters compose,
// so [WithPlatform] and [WithBrowser] together select their intersection; a
// combination that no generator covers, such as Samsung Internet on desktop,
// returns an empty slice. [Random] relaxes such a combination instead.
//
// The returned slice is a copy: mutating it never affects the package.
func List(opts ...OptionFunc) (userAgents []string) {
	filter := resolveOptions(opts)
	entries := matchingEntries(filter)

	userAgents = make([]string, 0, len(entries))

	for _, current := range entries {
		userAgents = append(userAgents, current.userAgent)
	}

	return userAgents
}

// Random returns a random User-Agent value that matches the given filters.
//
// With no options it draws from the entire generated catalog. When the
// filters match nothing, the browser constraint is dropped first; if the
// platform alone still matches nothing, both constraints are ignored. Random
// therefore always returns a value, unlike [List], which reports an exact
// empty match.
//
// The draw uses a pseudorandom source, not a cryptographic one: it suits
// request rotation, not security decisions.
func Random(opts ...OptionFunc) (userAgent string) {
	filter := resolveOptions(opts)
	entries := matchingEntries(filter)

	if len(entries) == 0 && filter.Browser != "" {
		filter.Browser = ""
		entries = matchingEntries(filter)
	}

	if len(entries) == 0 && filter.Platform != "" {
		filter.Platform = ""
		entries = matchingEntries(filter)
	}

	if len(entries) == 0 {
		entries = catalog
	}

	//nolint:gosec // The draw only rotates request cosmetics; predictability is not a security property here.
	userAgent = entries[rand.IntN(len(entries))].userAgent

	return userAgent
}

// RandomDesktop returns a random desktop User-Agent.
//
// The platform is always desktop; the caller's browser filter still applies,
// and [Random]'s fallback rules cover a browser that has no desktop variants.
func RandomDesktop(opts ...OptionFunc) (userAgent string) {
	userAgent = randomForPlatform(PlatformDesktop, opts...)

	return userAgent
}

// RandomMobile returns a random mobile User-Agent.
//
// The platform is always mobile; the caller's browser filter still applies,
// and [Random]'s fallback rules cover a browser that has no mobile variants.
func RandomMobile(opts ...OptionFunc) (userAgent string) {
	userAgent = randomForPlatform(PlatformMobile, opts...)

	return userAgent
}

// randomForPlatform forces a platform and delegates the draw to [Random].
func randomForPlatform(platform Platform, opts ...OptionFunc) (userAgent string) {
	forced := make([]OptionFunc, 0, len(opts)+1)

	forced = append(forced, opts...)
	forced = append(forced, WithPlatform(platform))

	userAgent = Random(forced...)

	return userAgent
}
