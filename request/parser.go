package request

import (
	"errors"
	"net/url"
)

// URLParser parses a raw URL string into the absolute URL a request is built
// from. It is the minimal surface the request builder needs: a parser from
// github.com/hueristiq/hq-lib-url-go can be adapted with [URLParserFunc], so no
// module dependency is imposed.
//
// Methods:
//   - Parse(raw string) (parsed *url.URL, err error): converts a raw URL string
//     into an absolute URL.
type URLParser interface {
	// Parse converts a raw URL string into an absolute URL.
	//
	// Parameters:
	//   - raw (string): the raw URL string to parse.
	//
	// Returns:
	//   - parsed (*url.URL): the parsed URL; it must carry a scheme and a host.
	//   - err (error): non-nil when the URL cannot be parsed.
	Parse(raw string) (parsed *url.URL, err error)
}

// URLParserFunc adapts an ordinary function to a [URLParser]. It is the place
// to bridge a parser whose method set does not match the interface — for
// example hq-lib-url-go's parser — without imposing a module dependency.
type URLParserFunc func(raw string) (parsed *url.URL, err error)

// Compile-time guard ensuring [URLParserFunc] satisfies [URLParser].
var _ URLParser = URLParserFunc(nil)

// Parse calls f(raw).
//
// Parameters:
//   - raw (string): the raw URL string to parse.
//
// Returns:
//   - parsed (*url.URL): whatever f returns.
//   - err (error): whatever f returns, or a nil-parser error when f is nil.
func (f URLParserFunc) Parse(raw string) (parsed *url.URL, err error) {
	if f == nil {
		return nil, errors.New("request.URLParserFunc: nil parser")
	}

	return f(raw)
}

// Options configures how [New], [NewLimited], and [NewStream] build a request.
//
// Fields:
//   - Parser (URLParser): the parser used to turn the raw URL string into an
//     absolute URL. Nil selects [net/url.Parse].
type Options struct {
	Parser URLParser
}

// OptionFunc configures an [Options] value. A nil OptionFunc is ignored.
// Multiple options can be combined; when the same setting is supplied more
// than once, the last value wins. The With* helpers cover common settings, and
// callers may write custom options because [Options] is exported.
//
// Parameters:
//   - opts (*Options): the options to modify.
type OptionFunc func(opts *Options)

// WithURLParser returns an [OptionFunc] that sets the parser converting the
// raw URL string into an absolute URL.
//
// The default is [net/url.Parse], which is strict: it applies no default
// scheme and repairs no invalid percent escapes. Supply a parser to add either
// behavior — for example an adapter around the parser from
// github.com/hueristiq/hq-lib-url-go built with [URLParserFunc].
//
// Parameters:
//   - parser (URLParser): the parser. Nil keeps the default.
//
// Returns:
//   - fn (OptionFunc): a functional option that sets the Parser field in the
//     options.
func WithURLParser(parser URLParser) (fn OptionFunc) {
	return func(opts *Options) {
		if parser != nil {
			opts.Parser = parser
		}
	}
}

// resolveParser applies the options and returns the parser to use, falling
// back to [net/url.Parse] when none was supplied.
//
// Parameters:
//   - ofs ([]OptionFunc): the functional options to apply.
//
// Returns:
//   - parser (URLParser): the resolved parser, never nil.
func resolveParser(ofs []OptionFunc) (parser URLParser) {
	opts := &Options{}

	for _, f := range ofs {
		if f != nil {
			f(opts)
		}
	}

	if opts.Parser == nil {
		parser = URLParserFunc(url.Parse)
	} else {
		parser = opts.Parser
	}

	return parser
}
