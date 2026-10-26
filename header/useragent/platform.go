package useragent

// Platform identifies the device class a generated User-Agent advertises.
//
// The zero value is empty and means no platform filter: [Random] and [List]
// consider every platform.
type Platform string

// String returns the platform name.
//
// Returns:
//   - platform (string): the platform name.
func (p Platform) String() (platform string) {
	platform = string(p)

	return platform
}

const (
	// PlatformDesktop identifies desktop and laptop browsers.
	PlatformDesktop Platform = "desktop"
	// PlatformMobile identifies phone and tablet browsers.
	PlatformMobile Platform = "mobile"
)
