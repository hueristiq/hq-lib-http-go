package useragent

// safariDesktopRelease pairs a Safari version with the macOS releases that
// support it.
type safariDesktopRelease struct {
	version string
	macOS   []string
}

// safariDesktopReleases lists recent Safari desktop releases and their
// supported macOS platforms.
//
// Data sources:
//   - https://support.apple.com/en-us/100100
//   - https://developer.apple.com/documentation/safari-release-notes
var safariDesktopReleases = []safariDesktopRelease{
	{
		version: "27.0",
		macOS: []string{
			"Macintosh; Intel Mac OS X 15_7",
			"Macintosh; Intel Mac OS X 26_6",
			"Macintosh; Intel Mac OS X 27_0",
		},
	},
	{
		version: "26.6",
		macOS: []string{
			"Macintosh; Intel Mac OS X 15_7",
			"Macintosh; Intel Mac OS X 26_6",
		},
	},
	{
		version: "26.5",
		macOS: []string{
			"Macintosh; Intel Mac OS X 15_7",
			"Macintosh; Intel Mac OS X 26_6",
		},
	},
	{
		version: "26.4",
		macOS: []string{
			"Macintosh; Intel Mac OS X 15_7",
			"Macintosh; Intel Mac OS X 26_6",
		},
	},
	{
		version: "26.3",
		macOS: []string{
			"Macintosh; Intel Mac OS X 15_7",
			"Macintosh; Intel Mac OS X 26_6",
		},
	},
	{
		version: "26.2",
		macOS: []string{
			"Macintosh; Intel Mac OS X 15_7",
			"Macintosh; Intel Mac OS X 26_6",
		},
	},
	{
		version: "26.1",
		macOS: []string{
			"Macintosh; Intel Mac OS X 15_7",
			"Macintosh; Intel Mac OS X 26_6",
		},
	},
	{
		version: "26.0",
		macOS: []string{
			"Macintosh; Intel Mac OS X 15_7",
			"Macintosh; Intel Mac OS X 26_6",
		},
	},
	{
		version: "18.6",
		macOS: []string{
			"Macintosh; Intel Mac OS X 13_6",
			"Macintosh; Intel Mac OS X 14_7",
			"Macintosh; Intel Mac OS X 15_7",
		},
	},
}

// genSafariDesktop generates Safari desktop User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genSafariDesktop() (userAgents []string) {
	count := 0

	for _, release := range safariDesktopReleases {
		count += len(release.macOS)
	}

	userAgents = make([]string, 0, count)

	for _, release := range safariDesktopReleases {
		for _, macOS := range release.macOS {
			userAgents = append(userAgents, safariDesktopUA(macOS, release.version))
		}
	}

	return userAgents
}

// genSafariIOS generates Safari on iOS User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genSafariIOS() (userAgents []string) {
	userAgents = make([]string, 0, iosCombinationCount())

	for _, device := range iosDevices {
		for _, iosVersion := range device.versions {
			userAgents = append(userAgents, safariIOSUA(device, iosVersion, iosSafariVersions[iosVersion]))
		}
	}

	return userAgents
}
