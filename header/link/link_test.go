package link_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgohttplink "github.com/hueristiq/hq-lib-http-go/header/link"
)

func TestWebLink_String(t *testing.T) {
	t.Parallel()

	link := hqgohttplink.WebLink{
		URL: "http://example.com",
		Rel: "next",
		Params: map[string]string{
			"foo": "bar",
			"baz": "qux",
		},
	}

	str := link.String()

	// Parameter keys are emitted in sorted order, followed by rel.
	assert.Equal(t, `<http://example.com>; baz="qux"; foo="bar"; rel="next"`, str)
}

func TestWebLink_String_EmptyRelAndParams(t *testing.T) {
	t.Parallel()

	link := hqgohttplink.WebLink{
		URL:    "http://example.com",
		Rel:    "",
		Params: map[string]string{},
	}

	str := link.String()

	assert.Equal(t, "<http://example.com>", str)
}

func TestWebLink_HasParam(t *testing.T) {
	t.Parallel()

	link := hqgohttplink.WebLink{
		URL: "http://example.com",
		Params: map[string]string{
			"foo": "bar",
		},
	}

	assert.True(t, link.HasParam("foo"))
	assert.False(t, link.HasParam("baz"))
}

func TestWebLink_Param(t *testing.T) {
	t.Parallel()

	link := hqgohttplink.WebLink{
		URL: "http://example.com",
		Params: map[string]string{
			"foo": "bar",
		},
	}

	assert.Equal(t, "bar", link.Param("foo"))
	assert.Empty(t, link.Param("baz"))
}

func TestWebLinks_String(t *testing.T) {
	t.Parallel()

	links := hqgohttplink.WebLinks{
		{
			URL: "http://example.com",
			Rel: "next",
			Params: map[string]string{
				"foo": "bar",
			},
		},
		{
			URL: "http://example.org",
			Rel: "prev",
			Params: map[string]string{
				"baz": "qux",
			},
		},
	}

	str := links.String()

	assert.Contains(t, str, "<http://example.com>")
	assert.Contains(t, str, `foo="bar"`)
	assert.Contains(t, str, `rel="next"`)
	assert.Contains(t, str, "<http://example.org>")
	assert.Contains(t, str, `baz="qux"`)
	assert.Contains(t, str, `rel="prev"`)
	assert.Contains(t, str, ", ")
}

func TestWebLinks_String_Empty(t *testing.T) {
	t.Parallel()

	var links hqgohttplink.WebLinks

	assert.Empty(t, links.String())
}

func TestWebLinks_FilterByRel(t *testing.T) {
	t.Parallel()

	links := hqgohttplink.WebLinks{
		{
			URL: "http://example.com/next",
			Rel: "next",
		},
		{
			URL: "http://example.com/prev",
			Rel: "prev",
		},
		{
			URL: "http://example.com/also-next",
			Rel: "next",
		},
	}

	filtered := links.FilterByRel("next")

	assert.Len(t, filtered, 2)

	for _, link := range filtered {
		assert.Equal(t, "next", link.Rel)
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		expected hqgohttplink.WebLinks
	}{
		{
			name:     "empty",
			raw:      "",
			expected: hqgohttplink.WebLinks{},
		},
		{
			name: "single link",
			raw:  `<http://example.com>; rel="next"; foo="bar"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"foo": "bar"},
				},
			},
		},
		{
			name: "multiple links",
			raw:  `<http://example.com>; rel="next"; foo="bar", <http://example.org>; rel="prev"; baz="qux"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"foo": "bar"},
				},
				{
					URL:    "http://example.org",
					Rel:    "prev",
					Params: map[string]string{"baz": "qux"},
				},
			},
		},
		{
			name: "comma inside URL",
			raw:  `<http://example.com/a,b?x=1,y=2>; rel="next", <http://example.org/z>; rel="prev"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com/a,b?x=1,y=2",
					Rel:    "next",
					Params: map[string]string{},
				},
				{
					URL:    "http://example.org/z",
					Rel:    "prev",
					Params: map[string]string{},
				},
			},
		},
		{
			name: "unquoted values",
			raw:  `<http://example.com>; rel=next; type=text/html`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"type": "text/html"},
				},
			},
		},
		{
			name: "rel key is matched case-insensitively",
			raw:  `<http://example.com>; REL="next"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{},
				},
			},
		},
		{
			name: "parameter keys are kept as written",
			raw:  `<http://example.com>; rel="next"; Title="Next Page"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"Title": "Next Page"},
				},
			},
		},
		{
			name: "parameter without value",
			raw:  `<http://example.com>; rel="next"; hreflang`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"hreflang": ""},
				},
			},
		},
		{
			name: "whitespace around separators",
			raw:  "  <http://example.com>  ;  rel=\"next\"  ,  <http://example.org>  ;  rel=\"prev\"  ",
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{},
				},
				{
					URL:    "http://example.org",
					Rel:    "prev",
					Params: map[string]string{},
				},
			},
		},
		{
			name: "entries without a URL are skipped",
			raw:  `rel="next", <http://example.com>; rel="next"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{},
				},
			},
		},
		{
			name:     "only invalid entries",
			raw:      `rel="next"`,
			expected: hqgohttplink.WebLinks{},
		},
		{
			name:     "blank header",
			raw:      "   ",
			expected: hqgohttplink.WebLinks{},
		},
		{
			name:     "empty entries",
			raw:      ",,",
			expected: hqgohttplink.WebLinks{},
		},
		{
			name: "empty parameters are skipped",
			raw:  `<http://example.com>;;rel="next"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{},
				},
			},
		},
		{
			name: "parameters without a key are skipped",
			raw:  `<http://example.com>; =value`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "",
					Params: map[string]string{},
				},
			},
		},
		{
			name: "comma inside a quoted value",
			raw:  `<http://example.com>; title="a,b"; rel="next"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"title": "a,b"},
				},
			},
		},
		{
			name: "semicolon inside a quoted value",
			raw:  `<http://example.com>; title="a;b"; rel="next"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"title": "a;b"},
				},
			},
		},
		{
			name: "semicolon inside the URL",
			raw:  `<http://example.com/a;b>; rel="next"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com/a;b",
					Rel:    "next",
					Params: map[string]string{},
				},
			},
		},
		{
			name: "escaped quotes and backslashes",
			raw:  `<http://example.com>; title="a \"quoted\" \\ value"; rel="next"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"title": `a "quoted" \ value`},
				},
			},
		},
		{
			name: "quoted comma separates multiple entries correctly",
			raw:  `<http://example.com>; title="a,b"; rel="next", <http://example.org>; rel="prev"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"title": "a,b"},
				},
				{
					URL:    "http://example.org",
					Rel:    "prev",
					Params: map[string]string{},
				},
			},
		},
		{
			name: "equals sign inside a quoted value",
			raw:  `<http://example.com>; title="a=b"; rel="next"`,
			expected: hqgohttplink.WebLinks{
				{
					URL:    "http://example.com",
					Rel:    "next",
					Params: map[string]string{"title": "a=b"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			links := hqgohttplink.Parse(tt.raw)

			assert.Equal(t, tt.expected, links)
		})
	}
}

func TestParseMultiple(t *testing.T) {
	t.Parallel()

	headers := []string{
		`<http://example.com>; rel="next"; foo="bar"`,
		`<http://example.org>; rel="prev"; baz="qux"`,
	}

	links := hqgohttplink.ParseMultiple(headers)

	require.Len(t, links, 2)

	assert.Equal(t, "http://example.com", links[0].URL)
	assert.Equal(t, "next", links[0].Rel)
	assert.Equal(t, "bar", links[0].Params["foo"])
	assert.Equal(t, "http://example.org", links[1].URL)
	assert.Equal(t, "prev", links[1].Rel)
	assert.Equal(t, "qux", links[1].Params["baz"])
}

func TestWebLink_RoundTripQuotedValues(t *testing.T) {
	t.Parallel()

	original := hqgohttplink.WebLink{
		URL: "http://example.com/a;b",
		Rel: "next",
		Params: map[string]string{
			"title": `a "quoted", b; c \ value`,
		},
	}

	links := hqgohttplink.Parse(original.String())

	require.Len(t, links, 1)
	assert.Equal(t, original, links[0])
}

func TestWebLink_String_DoesNotDoubleRel(t *testing.T) {
	t.Parallel()

	link := hqgohttplink.WebLink{
		URL: "http://example.com",
		Rel: "next",
		Params: map[string]string{
			"rel": "prev",
		},
	}

	str := link.String()

	assert.Equal(t, 1, strings.Count(str, "rel="))
	assert.Contains(t, str, `rel="next"`)
}

func TestWebLink_String_PreservesTabs(t *testing.T) {
	t.Parallel()

	link := hqgohttplink.WebLink{
		URL: "http://example.com",
		Params: map[string]string{
			"title": "a\tb",
		},
	}

	str := link.String()

	assert.Contains(t, str, "title=\"a\tb\"")

	links := hqgohttplink.Parse(str)

	require.Len(t, links, 1)
	assert.Equal(t, "a\tb", links[0].Param("title"))
}

func TestWebLinks_FilterByRel_CaseInsensitiveAndTokens(t *testing.T) {
	t.Parallel()

	links := hqgohttplink.WebLinks{
		{URL: "http://example.com/a", Rel: "Next"},
		{URL: "http://example.com/b", Rel: "next prev"},
		{URL: "http://example.com/c", Rel: "prev"},
	}

	assert.Len(t, links.FilterByRel("next"), 2)
	assert.Len(t, links.FilterByRel("NEXT"), 2)
	assert.Len(t, links.FilterByRel("prev"), 2)
	assert.Empty(t, links.FilterByRel("author"))
}

func TestParse_RepeatedParametersKeepFirst(t *testing.T) {
	t.Parallel()

	links := hqgohttplink.Parse(`<http://example.com>; rel="next"; rel="prev"; title="first"; title="second"`)

	require.Len(t, links, 1)
	assert.Equal(t, "next", links[0].Rel)
	assert.Equal(t, "first", links[0].Param("title"))
}

func TestWebLink_ParamCaseInsensitive(t *testing.T) {
	t.Parallel()

	link := hqgohttplink.WebLink{
		Rel:    "next",
		Params: map[string]string{"Title": "Next Page"},
	}

	assert.True(t, link.HasParam("title"))
	assert.Equal(t, "Next Page", link.Param("TITLE"))
	assert.True(t, link.HasParam("REL"))
	assert.Equal(t, "next", link.Param("Rel"))
}

func TestParse_EmptyURIReference(t *testing.T) {
	t.Parallel()

	links := hqgohttplink.Parse(`<>; rel="self"`)

	require.Len(t, links, 1)
	assert.Empty(t, links[0].URL)
	assert.Equal(t, "self", links[0].Rel)
}

func TestParse_CaseInsensitiveParameters(t *testing.T) {
	t.Parallel()

	links := hqgohttplink.Parse(`<https://example.com>; Title="first"; title="second"`)

	require.Len(t, links, 1)
	assert.Equal(t, "first", links[0].Param("title"))
	assert.True(t, links[0].HasParam("TITLE"))
	assert.Len(t, links[0].Params, 1)
}

func TestWebLink_StringSanitizesCRLF(t *testing.T) {
	t.Parallel()

	link := hqgohttplink.WebLink{
		URL:    "https://example.com/\r\nInjected: yes",
		Rel:    "next\r\n",
		Params: map[string]string{"title": "a\r\nb"},
	}

	formatted := link.String()

	assert.NotContains(t, formatted, "\r")
	assert.NotContains(t, formatted, "\n")
}
