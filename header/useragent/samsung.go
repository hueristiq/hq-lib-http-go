package useragent

import (
	"strings"
)

// samsungInternetRelease pairs a Samsung Internet version with the Chromium
// version it ships.
type samsungInternetRelease struct {
	chromium string
	version  string
}

// samsungInternetReleases lists recent stable Samsung Internet releases.
//
// Data sources:
//   - https://en.wikipedia.org/wiki/Samsung_Browser
//   - https://developer.samsung.com/internet
var samsungInternetReleases = []samsungInternetRelease{
	{chromium: "143", version: "30.0.5.27"},
	{chromium: "143", version: "30.0.2.45"},
	{chromium: "143", version: "30.0.0.25"},
	{chromium: "136", version: "29.0.4.46"},
	{chromium: "136", version: "29.0.0.59"},
	{chromium: "130", version: "28.0.2.43"},
	{chromium: "130", version: "28.0.0.55"},
}

// genSamsungInternetAndroid generates Samsung Internet User-Agents.
//
// Returns:
//   - userAgents ([]string): the generated User-Agents.
func genSamsungInternetAndroid() (userAgents []string) {
	count := 0

	for _, device := range androidDevices {
		if strings.HasPrefix(device.name, "SM-") {
			count += len(device.versions)
		}
	}

	userAgents = make([]string, 0, len(samsungInternetReleases)*count)

	for _, release := range samsungInternetReleases {
		for _, device := range androidDevices {
			if !strings.HasPrefix(device.name, "SM-") {
				continue
			}

			for _, android := range device.versions {
				userAgents = append(userAgents, samsungInternetUA(android, device.name, release.chromium, release.version))
			}
		}
	}

	return userAgents
}
