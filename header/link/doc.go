// Package link parses HTTP Link headers as defined by RFC 8288.
//
// A Link header carries one or more comma-separated entries, each holding a
// target URL plus parameters such as the relation type:
//
//	<https://api.example.com/page/2>; rel="next"; title="Next Page"
//
// # Entry points
//
// [Parse] parses one raw Link header value into a [WebLinks] collection of
// [WebLink] values — target URL, relation type, and parameters.
// [ParseMultiple] merges several Link header lines into one collection.
//
// Parameter keys are kept exactly as written, and the "rel" attribute is
// matched case-insensitively. Commas inside the angle-bracketed URL are
// preserved, and quoted parameter values containing a comma or a semicolon
// are handled. A repeated parameter keeps its first value. A space-separated
// rel value is stored as written and matched by [WebLinks.FilterByRel].
//
// The implementation relies on the Go standard library only.
package link
