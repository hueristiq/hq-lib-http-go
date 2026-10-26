package useragent

// Browser identifies the browser family a generated User-Agent advertises.
//
// The zero value is empty and means no browser filter: [Random] and [List]
// consider every browser.
type Browser string

// String returns the browser name.
//
// Returns:
//   - browser (string): the browser name.
func (b Browser) String() (browser string) {
	browser = string(b)

	return browser
}

const (
	// BrowserChrome identifies Chrome on desktop, Android, and iOS.
	BrowserChrome Browser = "chrome"
	// BrowserFirefox identifies Firefox on desktop, Android, and iOS.
	BrowserFirefox Browser = "firefox"
	// BrowserSafari identifies Safari on macOS and iOS.
	BrowserSafari Browser = "safari"
	// BrowserEdge identifies Edge on desktop, Android, and iOS.
	BrowserEdge Browser = "edge"
	// BrowserOpera identifies Opera on desktop, Android, and iOS.
	BrowserOpera Browser = "opera"
	// BrowserSamsungInternet identifies Samsung Internet on Android.
	BrowserSamsungInternet Browser = "samsung-internet"
)
