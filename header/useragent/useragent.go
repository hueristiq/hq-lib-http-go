package useragent

import (
	"math/rand/v2"
)

// entry is one User-Agent value together with the filters it answers to.
//
// Fields:
//   - userAgent (string): the generated User-Agent value.
//   - platform (Platform): the platform the value advertises.
//   - browser (Browser): the browser family the value advertises.
type entry struct {
	userAgent string
	platform  Platform
	browser   Browser
}

// Options holds the filters applied by [Random] and [List].
//
// Fields:
//   - Platform (Platform): the platform filter; the zero value means any.
//   - Browser (Browser): the browser filter; the zero value means any.
type Options struct {
	Platform Platform
	Browser  Browser
}

// Option configures a User-Agent draw or listing. The zero value of a filter
// means no constraint.
type Option func(*Options)

// filterKey identifies one combination of browser and platform filters.
//
// Fields:
//   - browser (Browser): the browser filter, or empty for any.
//   - platform (Platform): the platform filter, or empty for any.
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

// WithPlatform returns an [Option] that restricts the result to one platform.
//
// Parameters:
//   - platform (Platform): the platform to select.
//
// Returns:
//   - option (Option): the functional option.
func WithPlatform(platform Platform) (option Option) {
	option = func(opts *Options) {
		opts.Platform = platform
	}

	return option
}

// WithBrowser returns an [Option] that restricts the result to one browser.
//
// Parameters:
//   - browser (Browser): the browser to select.
//
// Returns:
//   - option (Option): the functional option.
func WithBrowser(browser Browser) (option Option) {
	option = func(opts *Options) {
		opts.Browser = browser
	}

	return option
}

// buildCatalog enumerates every generator and removes duplicate values.
//
// Returns:
//   - entries ([]entry): the deduplicated catalog.
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
//
// Parameters:
//   - entries ([]entry): the deduplicated catalog.
//
// Returns:
//   - index (map[filterKey][]entry): the per-filter entry slices.
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
//
// Parameters:
//   - opts ([]Option): the options to apply.
//
// Returns:
//   - resolved (Options): the resolved filters.
func resolveOptions(opts []Option) (resolved Options) {
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
//
// Parameters:
//   - filter (Options): the filters to apply.
//
// Returns:
//   - entries ([]entry): the matching entries, empty when none match.
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
//
// Parameters:
//   - opts (...Option): optional platform and browser filters.
//
// Returns:
//   - userAgents ([]string): the matching User-Agent values.
func List(opts ...Option) (userAgents []string) {
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
//
// Parameters:
//   - opts (...Option): optional platform and browser filters.
//
// Returns:
//   - userAgent (string): a random matching User-Agent value.
func Random(opts ...Option) (userAgent string) {
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
//
// Parameters:
//   - opts (...Option): optional browser filters.
//
// Returns:
//   - userAgent (string): a random desktop User-Agent value.
func RandomDesktop(opts ...Option) (userAgent string) {
	userAgent = randomForPlatform(PlatformDesktop, opts...)

	return userAgent
}

// RandomMobile returns a random mobile User-Agent.
//
// The platform is always mobile; the caller's browser filter still applies,
// and [Random]'s fallback rules cover a browser that has no mobile variants.
//
// Parameters:
//   - opts (...Option): optional browser filters.
//
// Returns:
//   - userAgent (string): a random mobile User-Agent value.
func RandomMobile(opts ...Option) (userAgent string) {
	userAgent = randomForPlatform(PlatformMobile, opts...)

	return userAgent
}

// randomForPlatform forces a platform and delegates the draw to [Random].
//
// Parameters:
//   - platform (Platform): the platform to force.
//   - opts (...Option): optional browser filters.
//
// Returns:
//   - userAgent (string): a random User-Agent value for the platform.
func randomForPlatform(platform Platform, opts ...Option) (userAgent string) {
	forced := make([]Option, 0, len(opts)+1)

	forced = append(forced, opts...)
	forced = append(forced, WithPlatform(platform))

	userAgent = Random(forced...)

	return userAgent
}
