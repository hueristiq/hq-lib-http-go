package useragent

// firefoxVersions lists recent stable Firefox releases. Firefox reports the
// major version in its User-Agent, so the patch release is omitted.
//
// Data source: https://product-details.mozilla.org/1.0/firefox_versions.json
var firefoxVersions = []string{
	"158.0",
	"157.0",
	"156.0",
	"155.0",
	"154.0",
	"153.0",
	"152.0",
	"151.0",
	"150.0",
	"149.0",
	"148.0",
	"147.0",
	"146.0",
	"145.0",
}

// genFirefoxDesktop generates Firefox desktop User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genFirefoxDesktop() (userAgents []string) {
	userAgents = make([]string, 0, len(firefoxVersions)*len(desktopOS))

	for _, version := range firefoxVersions {
		for _, platformOS := range desktopOS {
			userAgents = append(userAgents, firefoxDesktopUA(platformOS, version))
		}
	}

	return userAgents
}

// genFirefoxAndroid generates Firefox for Android User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genFirefoxAndroid() (userAgents []string) {
	userAgents = make([]string, 0, len(firefoxVersions)*androidCombinationCount())

	for _, version := range firefoxVersions {
		for _, device := range androidDevices {
			for _, android := range device.versions {
				userAgents = append(userAgents, firefoxAndroidUA(android, version))
			}
		}
	}

	return userAgents
}

// genFirefoxIOS generates Firefox for iOS User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genFirefoxIOS() (userAgents []string) {
	userAgents = make([]string, 0, len(firefoxVersions)*iosCombinationCount())

	for _, version := range firefoxVersions {
		for _, device := range iosDevices {
			for _, iosVersion := range device.versions {
				userAgents = append(userAgents, firefoxIOSUA(device, iosVersion, version))
			}
		}
	}

	return userAgents
}
