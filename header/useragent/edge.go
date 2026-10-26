package useragent

// edgeVersions lists recent stable Edge major versions. Edge aligns its major
// version with the Chromium version it ships.
//
// Data source: https://edgeupdates.microsoft.com/api/products?view=enterprise
var edgeVersions = []string{
	"156",
	"155",
	"154",
	"153",
	"152",
	"151",
	"150",
	"149",
	"148",
}

// genEdgeDesktop generates Edge desktop User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genEdgeDesktop() (userAgents []string) {
	userAgents = make([]string, 0, len(edgeVersions)*len(desktopOS))

	for _, version := range edgeVersions {
		for _, platformOS := range desktopOS {
			userAgents = append(userAgents, edgeDesktopUA(platformOS, version))
		}
	}

	return userAgents
}

// genEdgeAndroid generates Edge for Android User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genEdgeAndroid() (userAgents []string) {
	userAgents = make([]string, 0, len(edgeVersions)*androidCombinationCount())

	for _, version := range edgeVersions {
		for _, device := range androidDevices {
			for _, android := range device.versions {
				userAgents = append(userAgents, edgeAndroidUA(android, device.name, version))
			}
		}
	}

	return userAgents
}

// genEdgeIOS generates Edge for iOS User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genEdgeIOS() (userAgents []string) {
	userAgents = make([]string, 0, len(edgeVersions)*iosCombinationCount())

	for _, version := range edgeVersions {
		for _, device := range iosDevices {
			for _, iosVersion := range device.versions {
				userAgents = append(userAgents, edgeIOSUA(device, iosVersion, version))
			}
		}
	}

	return userAgents
}
