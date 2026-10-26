package useragent

// operaDesktopRelease pairs a desktop Opera major version with the Chromium
// version it ships.
type operaDesktopRelease struct {
	chromium string
	opera    string
}

// operaDesktopReleases lists recent stable desktop Opera releases.
//
// Data sources:
//   - https://blogs.opera.com/desktop/
//   - https://blogs.opera.com/desktop/changelog-for-138/
var operaDesktopReleases = []operaDesktopRelease{
	{chromium: "154", opera: "138"},
	{chromium: "153", opera: "137"},
	{chromium: "152", opera: "136"},
	{chromium: "151", opera: "135"},
}

// operaAndroidRelease pairs an Opera for Android version with the Chromium
// version it ships.
type operaAndroidRelease struct {
	chromium string
	opera    string
}

// operaAndroidReleases lists recent stable Opera for Android releases.
//
// Data sources:
//   - https://apkpure.com/opera-browser/com.opera.browser
//   - https://opera-browser.en.uptodown.com/android
var operaAndroidReleases = []operaAndroidRelease{
	{chromium: "154", opera: "103.1.5234.90883"},
	{chromium: "154", opera: "103.0.5234.90813"},
	{chromium: "152", opera: "102.4.5206.90738"},
}

// operaIOSVersions lists recent stable Opera for iOS releases.
//
// Data source: https://apps.apple.com/us/app/opera-browser/id1411869974
var operaIOSVersions = []string{
	"6.7.3",
}

// genOperaDesktop generates Opera desktop User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genOperaDesktop() (userAgents []string) {
	userAgents = make([]string, 0, len(operaDesktopReleases)*len(desktopOS))

	for _, release := range operaDesktopReleases {
		for _, platformOS := range desktopOS {
			userAgents = append(userAgents, operaDesktopUA(platformOS, release.chromium, release.opera))
		}
	}

	return userAgents
}

// genOperaAndroid generates Opera for Android User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genOperaAndroid() (userAgents []string) {
	userAgents = make([]string, 0, len(operaAndroidReleases)*androidCombinationCount())

	for _, release := range operaAndroidReleases {
		for _, device := range androidDevices {
			for _, android := range device.versions {
				userAgents = append(userAgents, operaAndroidUA(android, device.name, release.chromium, release.opera))
			}
		}
	}

	return userAgents
}

// genOperaIOS generates Opera for iOS User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genOperaIOS() (userAgents []string) {
	userAgents = make([]string, 0, len(operaIOSVersions)*iosCombinationCount())

	for _, version := range operaIOSVersions {
		for _, device := range iosDevices {
			for _, iosVersion := range device.versions {
				userAgents = append(userAgents, operaIOSUA(device, iosVersion, version))
			}
		}
	}

	return userAgents
}
