package link

import (
	"fmt"
	"sort"
	"strings"
)

// WebLink is a single link parsed from an HTTP Link header, as defined by RFC
// 8288.
//
// It encapsulates the target URL, the relation type ("rel"), and any additional
// parameters associated with the link. The structure is designed to facilitate
// easy access to link components and to support formatting back into a valid
// Link header string.
//
// Fields:
//   - URL (string): the target URI for the link (e.g., "https://api.example.com/page/2").
//   - Rel (string): the link relation type (e.g., "next", "prev", "author").
//   - Params (map[string]string): a map of additional key/value pairs associated
//     with the link, with keys kept as written in the header (e.g.,
//     {"title": "Next Page"}). Parameter names are compared case-insensitively;
//     a repeated parameter keeps its first spelling and value. Parameters that
//     allow multiple values, such as hreflang, collapse to the first one.
type WebLink struct {
	URL    string
	Rel    string
	Params map[string]string
}

// String returns a string representation of the WebLink in the format
// specified by RFC 8288:
//
//	<URL>; key1="value1"; key2="value2"; rel="relation"
//
// The URL is enclosed in angle brackets (`<` and `>`), followed by semicolon-separated
// parameters. Parameter keys are emitted in sorted order for a deterministic
// representation. If the Rel field is non-empty, it is appended as a `rel="value"`
// parameter. When there are no parameters at all, no trailing "; " is emitted.
// The method ensures proper quoting of parameter values to comply with
// HTTP Link header syntax. CR and LF characters in the URL, keys, and values
// are replaced with spaces so the result stays a single header value.
//
// Returns:
//   - link (string): a formatted string representation of the WebLink.
func (l WebLink) String() (link string) {
	keys := make([]string, 0, len(l.Params))

	for key := range l.Params {
		if strings.EqualFold(key, "rel") {
			continue
		}

		keys = append(keys, key)
	}

	sort.Strings(keys)

	params := make([]string, 0, len(keys)+1)

	for _, key := range keys {
		params = append(params, sanitizeLinkToken(key)+"="+quoteLinkValue(l.Params[key]))
	}

	if l.Rel != "" {
		params = append(params, "rel="+quoteLinkValue(l.Rel))
	}

	if len(params) == 0 {
		link = fmt.Sprintf("<%s>", sanitizeLinkToken(l.URL))
	} else {
		link = fmt.Sprintf("<%s>; %s", sanitizeLinkToken(l.URL), strings.Join(params, "; "))
	}

	return link
}

// HasParam checks whether the Link contains a parameter with the specified key.
//
// Parameter names are matched case-insensitively. The "rel" parameter is stored
// in [WebLink.Rel] and is reported when it is non-empty.
//
// Parameters:
//   - key (string): the parameter name to check for.
//
// Returns:
//   - hasParam (bool): true if the parameter exists, false otherwise.
func (l WebLink) HasParam(key string) (hasParam bool) {
	if strings.EqualFold(key, "rel") {
		return l.Rel != ""
	}

	for existing := range l.Params {
		if strings.EqualFold(existing, key) {
			return true
		}
	}

	return false
}

// Param retrieves the value of the parameter identified by the provided key.
//
// If the parameter does not exist, an empty string is returned. Parameter names
// are matched case-insensitively. The "rel" parameter is stored in
// [WebLink.Rel] and is returned when it is non-empty.
//
// Parameters:
//   - key (string): the parameter name to retrieve.
//
// Returns:
//   - param (string): the value of the parameter, or an empty string if not found.
func (l WebLink) Param(key string) (param string) {
	if strings.EqualFold(key, "rel") {
		return l.Rel
	}

	for existing, value := range l.Params {
		if strings.EqualFold(existing, key) {
			return value
		}
	}

	return ""
}

// WebLinks is a slice of WebLink values, representing a collection of links.
//
// This type is used to store multiple links parsed from one or more HTTP Link
// headers, such as those used in pagination or hypermedia APIs. It provides
// methods for formatting the collection as a string and filtering links by their
// relation type.
type WebLinks []WebLink

// String returns a single string representation of the WebLinks collection.
//
// Each WebLink is formatted using its String method, and the resulting strings
// are joined with a comma and a space (", ") to conform to the HTTP Link header
// format. If the slice is empty, an empty string is returned.
//
// Returns:
//   - links (string): a comma-separated string of all link representations.
func (l WebLinks) String() (links string) {
	if len(l) == 0 {
		return links
	}

	strs := make([]string, 0, len(l))

	for _, link := range l {
		strs = append(strs, link.String())
	}

	links = strings.Join(strs, ", ")

	return links
}

// FilterByRel returns a new WebLinks collection containing only the WebLink
// objects with a Rel value matching the provided rel argument.
//
// Relation types are compared case-insensitively, and a space-separated Rel
// value matches when any of its relation types matches. This method is useful
// for extracting links with specific relation types, such as "next" or "prev"
// for pagination, from a larger collection.
//
// Parameters:
//   - rel (string): the relation type to filter by (case-insensitive).
//
// Returns:
//   - links (WebLinks): a new WebLinks slice containing only links with a matching Rel value.
func (l WebLinks) FilterByRel(rel string) (links WebLinks) {
	links = make(WebLinks, 0, len(l))

	for _, link := range l {
		for _, candidate := range strings.Fields(link.Rel) {
			if strings.EqualFold(candidate, rel) {
				links = append(links, link)

				break
			}
		}
	}

	return links
}

// Parse parses a raw HTTP Link header string into a WebLinks collection.
//
// The input string may contain one or more comma-separated link entries, each in
// the format: `<URL>; param1="value1"; param2="value2"`. The function splits the
// input into individual links at commas that appear outside angle brackets, so a
// comma inside the URL (in its path or query) does not split an entry. It then
// extracts the URL (enclosed in angle brackets) and parses parameters, including
// the "rel" attribute. Parameter names are matched case-insensitively, and a
// repeated parameter keeps its first value. An empty URI reference (`<>`, the
// current document) is preserved. If the input is empty or contains only invalid
// entries, an empty non-nil WebLinks slice is returned.
//
// Parameters:
//   - raw (string): the raw HTTP Link header string to parse.
//
// Returns:
//   - links (WebLinks): a slice of WebLink objects representing the parsed links.
func Parse(raw string) (links WebLinks) {
	links = make(WebLinks, 0)

	if raw == "" {
		return links
	}

	raw = strings.TrimSpace(raw)

	for _, chunk := range splitLinkEntries(raw) {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}

		link := WebLink{
			URL:    "",
			Rel:    "",
			Params: make(map[string]string),
		}

		var (
			relSet  bool
			urlSeen bool
		)

		for _, piece := range splitLinkParameters(chunk) {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}

			if len(piece) >= 2 && piece[0] == '<' && piece[len(piece)-1] == '>' {
				if !urlSeen {
					link.URL = piece[1 : len(piece)-1]
					urlSeen = true
				}

				continue
			}

			key, val := parseParameter(piece)
			if key == "" {
				continue
			}

			if strings.EqualFold(key, "rel") {
				if !relSet {
					link.Rel = val
					relSet = true
				}

				continue
			}

			duplicate := false

			for existing := range link.Params {
				if strings.EqualFold(existing, key) {
					duplicate = true

					break
				}
			}

			if !duplicate {
				link.Params[key] = val
			}
		}

		if urlSeen {
			links = append(links, link)
		}
	}

	return links
}

// splitLinkEntries splits a raw Link header string into entries at commas
// outside angle brackets and quoted strings. Comma-separated link-values are
// the outermost structure of a Link header; commas inside a URL or a quoted
// parameter value are data.
//
// Parameters:
//   - raw (string): the raw Link header string.
//
// Returns:
//   - entries ([]string): the comma-separated entries, trimmed.
func splitLinkEntries(raw string) (entries []string) {
	start := 0
	inAngle := false
	inQuote := false
	escaped := false

	for i := range len(raw) {
		c := raw[i]

		switch {
		case escaped:
			escaped = false
		case inQuote && c == '\\':
			escaped = true
		case inQuote:
			if c == '"' {
				inQuote = false
			}
		case c == '"':
			inQuote = true
		case c == '<':
			inAngle = true
		case c == '>':
			inAngle = false
		case c == ',' && !inAngle:
			entries = append(entries, strings.TrimSpace(raw[start:i]))
			start = i + 1
		}
	}

	entries = append(entries, strings.TrimSpace(raw[start:]))

	return entries
}

// splitLinkParameters splits one link-value into its URL and parameter
// pieces. Semicolons inside the angle-bracketed URL or a quoted parameter
// value do not split.
//
// Parameters:
//   - raw (string): One link-value, trimmed.
//
// Returns:
//   - pieces ([]string): the semicolon-separated pieces, trimmed.
func splitLinkParameters(raw string) (pieces []string) {
	start := 0
	inAngle := false
	inQuote := false
	escaped := false

	for i := range len(raw) {
		c := raw[i]

		switch {
		case escaped:
			escaped = false
		case inQuote && c == '\\':
			escaped = true
		case inQuote:
			if c == '"' {
				inQuote = false
			}
		case c == '"':
			inQuote = true
		case c == '<':
			inAngle = true
		case c == '>':
			inAngle = false
		case c == ';' && !inAngle:
			pieces = append(pieces, strings.TrimSpace(raw[start:i]))
			start = i + 1
		}
	}

	pieces = append(pieces, strings.TrimSpace(raw[start:]))

	return pieces
}

// parseParameter parses a raw parameter string in the format "key=value".
//
// It extracts the key and value, unquoting a quoted-string value and honoring
// its backslash escapes. A parameter without "=" yields an empty value. The
// function is unexported, as it is an internal helper used by Parse.
//
// Parameters:
//   - raw (string): the raw parameter string to parse (e.g., `rel="next"`,
//     `title="Next Page"`); must not be empty.
//
// Returns:
//   - key (string): the parsed parameter name.
//   - value (string): the parsed parameter value, with quotes and escapes
//     resolved.
func parseParameter(raw string) (key, value string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	key, value, found := strings.Cut(raw, "=")

	key = strings.TrimSpace(key)

	if !found {
		return key, ""
	}

	value = strings.TrimSpace(value)

	if unquoted, ok := unquoteLinkValue(value); ok {
		value = unquoted
	}

	return key, value
}

// ParseMultiple is like Parse, but accepts a slice of header values rather
// than just one header string.
//
// This function is useful when Link headers are spread across multiple header
// lines, as allowed by HTTP specifications (e.g., multiple "Link" headers in an
// HTTP response). It parses each header string using Parse and combines the
// results into a single WebLinks slice.
//
// Parameters:
//   - headers ([]string): a slice of raw HTTP Link header strings, each
//     potentially containing multiple links.
//
// Returns:
//   - links (WebLinks): a combined WebLinks slice containing all parsed WebLink objects.
func ParseMultiple(headers []string) (links WebLinks) {
	links = make(WebLinks, 0, len(headers))

	for _, header := range headers {
		links = append(links, Parse(header)...)
	}

	return links
}

// sanitizeLinkToken replaces CR and LF with spaces so a token cannot split the
// resulting header value.
//
// Parameters:
//   - value (string): the token to sanitize.
//
// Returns:
//   - sanitized (string): the token with CR and LF replaced.
func sanitizeLinkToken(value string) (sanitized string) {
	sanitized = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}

		return r
	}, value)

	return sanitized
}

// quoteLinkValue serializes value as an RFC 7230 quoted-string, escaping the
// quote and backslash characters and replacing CR and LF with spaces.
//
// Parameters:
//   - value (string): the parameter value.
//
// Returns:
//   - quoted (string): the quoted value.
func quoteLinkValue(value string) (quoted string) {
	var b strings.Builder

	b.WriteByte('"')

	for i := range len(value) {
		switch value[i] {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(value[i])
		case '\r', '\n':
			b.WriteByte(' ')
		default:
			b.WriteByte(value[i])
		}
	}

	b.WriteByte('"')

	quoted = b.String()

	return quoted
}

// unquoteLinkValue resolves an RFC 8288 quoted-string. It reports false when
// value is not a complete quoted string, in which case the caller keeps the
// value verbatim.
//
// Parameters:
//   - value (string): the raw parameter value.
//
// Returns:
//   - unquoted (string): the unescaped value.
//   - ok (bool): true when value was a complete quoted string.
func unquoteLinkValue(value string) (unquoted string, ok bool) {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", false
	}

	var b strings.Builder

	for i := 1; i < len(value)-1; i++ {
		if value[i] == '\\' {
			i++
			if i >= len(value)-1 {
				return "", false
			}
		}

		b.WriteByte(value[i])
	}

	unquoted = b.String()

	return unquoted, true
}
