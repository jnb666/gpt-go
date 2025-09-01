// Package markdown wraps the https://github.com/yuin/goldmark Markdown parser
package markdown

import (
	"bytes"
	"regexp"

	katex "github.com/FurqanSoftware/goldmark-katex"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	reBlock  = regexp.MustCompile(`(?s)\\\[(.+?)\\\]`)
	reInline = regexp.MustCompile(`\\\((.+?)\\\)`)
)

// Render markdown document to HTML
func Render(doc string) (string, error) {
	// convert \(...\) inline math to $...$ and \[...\] block math to $$...$$
	doc = reBlock.ReplaceAllString(doc, `$$$$$1$$$$`)
	doc = reInline.ReplaceAllString(doc, `$$$1$$`)
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			&katex.Extender{},
			highlighting.NewHighlighting(highlighting.WithStyle("monokai")),
		),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)

	var buf bytes.Buffer
	err := md.Convert([]byte(doc), &buf)
	return buf.String(), err
}
