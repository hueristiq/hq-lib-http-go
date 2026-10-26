// Package useragent generates User-Agent values for HTTP requests.
//
// It suits clients that should not announce themselves with a library default
// — crawlers, scrapers, security tooling — where blending in with ordinary
// browser traffic matters more than identifying the tool.
//
// # Entry points
//
// [List] returns every User-Agent value that matches optional filters;
// [Random] draws one uniformly from the same set. [RandomDesktop] and
// [RandomMobile] force the platform, and [WithPlatform] and [WithBrowser]
// narrow the catalog:
//
//	userAgent := useragent.Random(
//		useragent.WithPlatform(useragent.PlatformMobile),
//		useragent.WithBrowser(useragent.BrowserSafari),
//	)
//
// Filters compose. [List] reports an exact match, so a combination no
// generator covers returns an empty slice; [Random] relaxes the browser
// constraint first and then the platform constraint, so it always returns a
// value.
//
// # Catalog
//
// The package composes values from version, operating-system, and device
// tables instead of storing fixed strings. The catalog covers Chrome,
// Firefox, Safari, Edge, Opera, and Samsung Internet across desktop Windows,
// macOS, and Linux, Android phones and tablets, and iOS phones and tablets.
// Tables list recent stable releases and name their data sources in comments.
//
// The draw is pseudorandom, not cryptographic: it suits rotation, not
// security decisions.
//
// The implementation relies on the Go standard library only.
package useragent
