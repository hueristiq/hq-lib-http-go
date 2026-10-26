package useragent

import (
	"fmt"
)

// generator produces every User-Agent value for one browser and platform.
type generator struct {
	browser  Browser
	platform Platform
	all      func() []string
}

// generators is the registry that backs the catalog.
var generators = []generator{
	{browser: BrowserChrome, platform: PlatformDesktop, all: genChromeDesktop},
	{browser: BrowserChrome, platform: PlatformMobile, all: genChromeAndroid},
	{browser: BrowserChrome, platform: PlatformMobile, all: genChromeIOS},
	{browser: BrowserFirefox, platform: PlatformDesktop, all: genFirefoxDesktop},
	{browser: BrowserFirefox, platform: PlatformMobile, all: genFirefoxAndroid},
	{browser: BrowserFirefox, platform: PlatformMobile, all: genFirefoxIOS},
	{browser: BrowserSafari, platform: PlatformDesktop, all: genSafariDesktop},
	{browser: BrowserSafari, platform: PlatformMobile, all: genSafariIOS},
	{browser: BrowserEdge, platform: PlatformDesktop, all: genEdgeDesktop},
	{browser: BrowserEdge, platform: PlatformMobile, all: genEdgeAndroid},
	{browser: BrowserEdge, platform: PlatformMobile, all: genEdgeIOS},
	{browser: BrowserOpera, platform: PlatformDesktop, all: genOperaDesktop},
	{browser: BrowserOpera, platform: PlatformMobile, all: genOperaAndroid},
	{browser: BrowserOpera, platform: PlatformMobile, all: genOperaIOS},
	{browser: BrowserSamsungInternet, platform: PlatformMobile, all: genSamsungInternetAndroid},
}

// chromeDesktopUA renders a Chrome desktop User-Agent.
//
// Parameters:
//   - platformOS (string): the platform token, such as "Windows NT 10.0; Win64; x64".
//   - version (string): the full Chrome version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func chromeDesktopUA(platformOS, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", platformOS, version)

	return userAgent
}

// chromeAndroidUA renders a Chrome for Android User-Agent.
//
// Parameters:
//   - android (string): the Android release.
//   - device (string): the device name or model number.
//   - version (string): the full Chrome version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func chromeAndroidUA(android, device, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (Linux; Android %s; %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Mobile Safari/537.36", android, device, version)

	return userAgent
}

// chromeIOSUA renders a Chrome for iOS User-Agent.
//
// Parameters:
//   - device (iosDevice): the iOS device profile.
//   - iosVersion (string): the iOS version token, such as "27_1".
//   - version (string): the full Chrome version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func chromeIOSUA(device iosDevice, iosVersion, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/%s Mobile/15E148 Safari/604.1", iosDeviceToken(device, iosVersion), version)

	return userAgent
}

// firefoxDesktopUA renders a Firefox desktop User-Agent.
//
// Parameters:
//   - platformOS (string): the platform token.
//   - version (string): the Firefox version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func firefoxDesktopUA(platformOS, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s; rv:%s) Gecko/20100101 Firefox/%s", platformOS, version, version)

	return userAgent
}

// firefoxAndroidUA renders a Firefox for Android User-Agent.
//
// Parameters:
//   - android (string): the Android release.
//   - version (string): the Firefox version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func firefoxAndroidUA(android, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (Android %s; Mobile; rv:%s) Gecko/%s Firefox/%s", android, version, version, version)

	return userAgent
}

// firefoxIOSUA renders a Firefox for iOS User-Agent.
//
// Parameters:
//   - device (iosDevice): the iOS device profile.
//   - iosVersion (string): the iOS version token.
//   - version (string): the Firefox version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func firefoxIOSUA(device iosDevice, iosVersion, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/%s Mobile/15E148 Safari/605.1.15", iosDeviceToken(device, iosVersion), version)

	return userAgent
}

// safariDesktopUA renders a Safari desktop User-Agent.
//
// Parameters:
//   - macOS (string): the macOS platform token.
//   - version (string): the Safari version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func safariDesktopUA(macOS, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/%s Safari/605.1.15", macOS, version)

	return userAgent
}

// safariIOSUA renders a Safari on iOS User-Agent.
//
// Parameters:
//   - device (iosDevice): the iOS device profile.
//   - iosVersion (string): the iOS version token.
//   - version (string): the Safari version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func safariIOSUA(device iosDevice, iosVersion, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/%s Mobile/15E148 Safari/604.1", iosDeviceToken(device, iosVersion), version)

	return userAgent
}

// edgeDesktopUA renders an Edge desktop User-Agent.
//
// Parameters:
//   - platformOS (string): the platform token.
//   - version (string): the Edge major version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func edgeDesktopUA(platformOS, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36 Edg/%s.0.0.0", platformOS, version, version)

	return userAgent
}

// edgeAndroidUA renders an Edge for Android User-Agent.
//
// Parameters:
//   - android (string): the Android release.
//   - device (string): the device name or model number.
//   - version (string): the Edge major version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func edgeAndroidUA(android, device, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (Linux; Android %s; %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Mobile Safari/537.36 EdgA/%s.0.0.0", android, device, version, version)

	return userAgent
}

// edgeIOSUA renders an Edge for iOS User-Agent.
//
// Parameters:
//   - device (iosDevice): the iOS device profile.
//   - iosVersion (string): the iOS version token.
//   - version (string): the Edge major version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func edgeIOSUA(device iosDevice, iosVersion, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/605.1.15 (KHTML, like Gecko) EdgiOS/%s.0.0.0 Mobile/15E148 Safari/604.1", iosDeviceToken(device, iosVersion), version)

	return userAgent
}

// operaDesktopUA renders an Opera desktop User-Agent.
//
// Parameters:
//   - platformOS (string): the platform token.
//   - chromiumVersion (string): the Chromium major version.
//   - version (string): the Opera major version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func operaDesktopUA(platformOS, chromiumVersion, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36 OPR/%s.0.0.0", platformOS, chromiumVersion, version)

	return userAgent
}

// operaAndroidUA renders an Opera for Android User-Agent.
//
// Parameters:
//   - android (string): the Android release.
//   - device (string): the device name or model number.
//   - chromiumVersion (string): the Chromium major version.
//   - version (string): the Opera major version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func operaAndroidUA(android, device, chromiumVersion, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (Linux; Android %s; %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Mobile Safari/537.36 OPR/%s", android, device, chromiumVersion, version)

	return userAgent
}

// operaIOSUA renders an Opera for iOS User-Agent.
//
// Parameters:
//   - device (iosDevice): the iOS device profile.
//   - iosVersion (string): the iOS version token.
//   - version (string): the Opera version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func operaIOSUA(device iosDevice, iosVersion, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/605.1.15 (KHTML, like Gecko) OPiOS/%s Mobile/15E148 Safari/604.1", iosDeviceToken(device, iosVersion), version)

	return userAgent
}

// samsungInternetUA renders a Samsung Internet User-Agent.
//
// Parameters:
//   - android (string): the Android release.
//   - device (string): the device model number.
//   - chromiumVersion (string): the Chromium major version.
//   - version (string): the Samsung Internet version.
//
// Returns:
//   - userAgent (string): the rendered User-Agent.
func samsungInternetUA(android, device, chromiumVersion, version string) (userAgent string) {
	userAgent = fmt.Sprintf("Mozilla/5.0 (Linux; Android %s; %s) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/%s Chrome/%s.0.0.0 Mobile Safari/537.36", android, device, version, chromiumVersion)

	return userAgent
}

// iosDeviceToken renders the platform portion of an iOS User-Agent.
//
// Parameters:
//   - device (iosDevice): the iOS device profile.
//   - version (string): the iOS version token.
//
// Returns:
//   - token (string): the rendered platform token.
func iosDeviceToken(device iosDevice, version string) (token string) {
	if device.tablet {
		token = fmt.Sprintf("%s; CPU OS %s like Mac OS X", device.name, version)

		return token
	}

	token = fmt.Sprintf("%s; CPU iPhone OS %s like Mac OS X", device.name, version)

	return token
}
