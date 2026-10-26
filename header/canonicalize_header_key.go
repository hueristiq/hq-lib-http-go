package header

import "strings"

// CanonicalizeHeaderKey converts an HTTP header key to its canonical form.
//
// The canonical form of an HTTP header key, as recommended by RFC 7231 and RFC 9110,
// capitalizes the first letter of each word in a hyphen-separated header name and lowercases
// the rest (e.g., "content-type" becomes "Content-Type"). This function processes the input
// string character by character, ensuring that:
//   - The first character after a hyphen or at the start of the string is uppercase.
//   - All other characters are lowercase.
//   - Hyphens are preserved as separators.
//
// The casing follows net/textproto, not the IANA-preferred spelling: "ETag"
// becomes "Etag" and "WWW-Authenticate" becomes "Www-Authenticate". Header names
// are case-insensitive on the wire, so this is safe for transport, but the
// result can differ from the constants in this package.
//
// Parameters:
//   - key (string): the input header key to canonicalize (e.g., "content-type", "ACCEPT-RANGES").
//
// Returns:
//   - canonicalKey (string): the canonicalized header key (e.g., "Content-Type", "Accept-Ranges").
func CanonicalizeHeaderKey(key string) (canonicalKey string) {
	var b strings.Builder

	upper := true

	for i := range len(key) {
		c := key[i]

		if upper && 'a' <= c && c <= 'z' {
			c -= 'a' - 'A'
		} else if !upper && 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}

		b.WriteByte(c)

		upper = c == '-'
	}

	canonicalKey = b.String()

	return canonicalKey
}
