package useragent

// chromeVersions lists recent stable Chrome releases, one per major version.
//
// Data source: https://versionhistory.googleapis.com/v1/chrome/platforms/win64/channels/stable/versions
var chromeVersions = []string{
	"157.0.8103.15",
	"156.0.8078.49",
	"155.0.8059.86",
	"154.0.8037.100",
	"153.0.8010.55",
	"152.0.7977.85",
	"151.0.7922.176",
	"150.0.7871.189",
	"149.0.7827.201",
	"148.0.7778.218",
	"147.0.7727.139",
	"146.0.7680.180",
	"145.0.7632.162",
	"144.0.7559.135",
}

// genChromeDesktop generates Chrome desktop User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genChromeDesktop() (userAgents []string) {
	userAgents = make([]string, 0, len(chromeVersions)*len(desktopOS))

	for _, version := range chromeVersions {
		for _, platformOS := range desktopOS {
			userAgents = append(userAgents, chromeDesktopUA(platformOS, version))
		}
	}

	return userAgents
}

// genChromeAndroid generates Chrome for Android User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genChromeAndroid() (userAgents []string) {
	userAgents = make([]string, 0, len(chromeVersions)*androidCombinationCount())

	for _, version := range chromeVersions {
		for _, device := range androidDevices {
			for _, android := range device.versions {
				userAgents = append(userAgents, chromeAndroidUA(android, device.name, version))
			}
		}
	}

	return userAgents
}

// genChromeIOS generates Chrome for iOS User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genChromeIOS() (userAgents []string) {
	userAgents = make([]string, 0, len(chromeVersions)*iosCombinationCount())

	for _, version := range chromeVersions {
		for _, device := range iosDevices {
			for _, iosVersion := range device.versions {
				userAgents = append(userAgents, chromeIOSUA(device, iosVersion, version))
			}
		}
	}

	return userAgents
}
