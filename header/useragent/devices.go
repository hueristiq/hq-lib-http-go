package useragent

// desktopOS lists the platform tokens shared by the desktop generators.
//
// Data sources:
//   - https://learn.microsoft.com/en-us/windows/release-health/
//   - https://support.apple.com/en-us/100100
//   - https://www.kernel.org/
var desktopOS = []string{
	// Windows 10 and 11 both report Windows NT 10.0.
	"Windows NT 10.0; Win64; x64",
	"Windows NT 10.0; WOW64",
	"Windows NT 10.0; ARM64",
	// macOS versions currently receiving browser updates, plus the frozen
	// Intel token Chrome and Safari still report.
	"Macintosh; Intel Mac OS X 10_15_7",
	"Macintosh; Intel Mac OS X 14_7",
	"Macintosh; Intel Mac OS X 15_7",
	"Macintosh; Intel Mac OS X 26_6",
	"Macintosh; Intel Mac OS X 27_0",
	// Common Linux distribution tokens.
	"X11; Linux x86_64",
	"X11; Ubuntu; Linux x86_64",
	"X11; Fedora; Linux x86_64",
}

// androidDevice is an Android device profile and the Android releases it
// plausibly runs.
type androidDevice struct {
	name     string
	versions []string
}

// androidDevices lists current phones and tablets.
//
// Data sources:
//   - https://developer.android.com/about/versions
//   - https://endoflife.date/android
//   - device launch announcements and model numbers
var androidDevices = []androidDevice{
	{name: "Pixel 9 Pro", versions: []string{"14", "15", "16", "17"}},
	{name: "Pixel 9", versions: []string{"14", "15", "16", "17"}},
	{name: "Pixel 8", versions: []string{"14", "15", "16", "17"}},
	{name: "Pixel 7", versions: []string{"13", "14", "15", "16", "17"}},
	{name: "SM-S938B", versions: []string{"15", "16", "17"}},
	{name: "SM-S921B", versions: []string{"14", "15", "16", "17"}},
	{name: "SM-S911B", versions: []string{"13", "14", "15", "16", "17"}},
	{name: "CPH2649", versions: []string{"15", "16", "17"}},
	{name: "23127PN0CC", versions: []string{"14", "15", "16", "17"}},
	{name: "Pixel Tablet", versions: []string{"14", "15", "16", "17"}},
	{name: "SM-X926B", versions: []string{"14", "15", "16", "17"}},
}

// iosDevice is an iOS device profile and the iOS releases it plausibly runs.
type iosDevice struct {
	name     string
	tablet   bool
	versions []string
}

// iosDevices lists current phones and tablets.
//
// Data sources:
//   - https://endoflife.date/ios
//   - Apple device compatibility pages
var iosDevices = []iosDevice{
	{name: "iPhone 17 Pro Max", versions: []string{"26_7", "27_0", "27_1"}},
	{name: "iPhone 17", versions: []string{"26_7", "27_0", "27_1"}},
	{name: "iPhone 16 Pro", versions: []string{"18_7", "26_7", "27_0", "27_1"}},
	{name: "iPhone 15", versions: []string{"17_7", "18_7", "26_7", "27_0", "27_1"}},
	{name: "iPhone 14", versions: []string{"16_7", "17_7", "18_7", "26_7", "27_0", "27_1"}},
	{name: "iPad Pro 13-inch (M4)", tablet: true, versions: []string{"17_7", "18_7", "26_7", "27_0", "27_1"}},
	{name: "iPad Air 11-inch (M3)", tablet: true, versions: []string{"17_7", "18_7", "26_7", "27_0", "27_1"}},
	{name: "iPad (11th generation)", tablet: true, versions: []string{"17_7", "18_7", "26_7", "27_0", "27_1"}},
}

// iosSafariVersions maps an iOS version token to the Safari version shipped
// with it.
//
// Data source: https://support.apple.com/en-us/100100
var iosSafariVersions = map[string]string{
	"27_1": "27.0",
	"27_0": "27.0",
	"26_7": "26.6",
	"18_7": "18.6",
	"17_7": "17.7",
	"16_7": "16.6",
}

// androidCombinationCount returns the number of Android device/version pairs.
//
// Returns:
//   - count (int): the number of pairs.
func androidCombinationCount() (count int) {
	for _, device := range androidDevices {
		count += len(device.versions)
	}

	return count
}

// iosCombinationCount returns the number of iOS device/version pairs.
//
// Returns:
//   - count (int): the number of pairs.
func iosCombinationCount() (count int) {
	for _, device := range iosDevices {
		count += len(device.versions)
	}

	return count
}
