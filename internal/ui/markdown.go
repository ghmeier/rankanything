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

// Goldmark renders; bluemonday strips dangerous links and tags.
var (
	markdown       = goldmark.New(goldmark.WithExtensions(extension.GFM))
	markdownHTML   = bluemonday.UGCPolicy()
	markdownStrict = bluemonday.StrictPolicy()
)

func Markdown(source string) templ.Component {
	rendered, err := renderMarkdown(source)
	if err != nil {
		return templ.Raw(html.EscapeString(source))
	}
	return templ.Raw(markdownHTML.Sanitize(rendered))
}

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
