package ui

import (
	"bytes"
	"html"
	"strings"

	"github.com/a-h/templ"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// Rendering user markdown takes two passes. Goldmark leaves raw HTML escaped
// because WithUnsafe is off, but it does not care what a link points at, so a
// [click](javascript:...) survives it intact — bluemonday is what removes
// that. markdownText strips every tag instead, for the places that need words
// rather than markup.
var (
	markdown       = goldmark.New(goldmark.WithExtensions(extension.GFM))
	markdownHTML   = bluemonday.UGCPolicy()
	markdownStrict = bluemonday.StrictPolicy()
)

// Markdown renders user-written markdown as sanitized HTML. Wrap it in
// .markdown-body, which is where the prose styling lives (input.css).
func Markdown(source string) templ.Component {
	rendered, err := renderMarkdown(source)
	if err != nil {
		// Convert only fails on a writer error, and this one writes to
		// memory. Showing the source beats showing nothing.
		return templ.Raw(html.EscapeString(source))
	}
	return templ.Raw(markdownHTML.Sanitize(rendered))
}

// PlainText flattens markdown to one line of words. The meta description and
// the Open Graph tags are attribute values, so markup in them reads as broken
// rather than as formatting.
func PlainText(source string) string {
	rendered, err := renderMarkdown(source)
	if err != nil {
		return source
	}
	stripped := html.UnescapeString(markdownStrict.Sanitize(rendered))
	return strings.Join(strings.Fields(stripped), " ")
}

func renderMarkdown(source string) (string, error) {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(source), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
