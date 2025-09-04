// Package markdown wraps the https://github.com/yuin/goldmark Markdown parser
package markdown

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	katex "github.com/FurqanSoftware/goldmark-katex"
	log "github.com/sirupsen/logrus"
	markdown "github.com/teekennedy/goldmark-markdown"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	htmlRenderer "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var (
	reBlock  = regexp.MustCompile(`(?s)\\\[(.+?)\\\]`)
	reInline = regexp.MustCompile(`\\\((.+?)\\\)`)
	reLink   = regexp.MustCompile(`(?i)(<a href="[^"]+")`)
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
		goldmark.WithRendererOptions(htmlRenderer.WithHardWraps()),
	)
	var buf bytes.Buffer
	err := md.Convert([]byte(doc), &buf)
	if err != nil {
		return "", err
	}
	return reLink.ReplaceAllString(buf.String(), `${1} target="_blank"`), nil
}

// Parsed Markdown content
type Document struct {
	Title      string
	URL        string
	Links      []Link
	Lines      []string
	StartLine  int
	WrapColumn int
}

// Link extracted from document
type Link struct {
	Title string
	URL   string
}

// Format link with id, title and host if different from srcHost.
func (l Link) Format(id int, srcHost string) string {
	host := URLHost(l.URL)
	if host != "" && srcHost != "" && host != srcHost {
		return fmt.Sprintf("【%d†%s†%s】", id, l.Title, host)
	} else {
		return fmt.Sprintf("【%d†%s】", id, l.Title)
	}
}

// Host part from URL
func URLHost(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		log.Warn("Error parsing url:", err)
		return ""
	}
	return u.Hostname()
}

// Parse source and replace links with 【<id>†<title>†<host>] format
func QuoteLinks(source, url, title string, wrapColumn int) Document {
	doc := Document{URL: url, Title: title, WrapColumn: wrapColumn}

	transformer := util.Prioritized(&linkTransformer{doc: &doc}, 0)
	md := goldmark.New(
		goldmark.WithRenderer(markdown.NewRenderer()),
		goldmark.WithParserOptions(parser.WithASTTransformers(transformer)),
	)
	var buf bytes.Buffer
	err := md.Convert([]byte(source), &buf)
	if err != nil {
		log.Error("QuoteLinks: markdown parsing error - ", err)
		doc.Write(source)
	} else {
		doc.Write(buf.String())
	}
	return doc
}

type linkTransformer struct {
	doc *Document
}

type linkNode struct {
	parent ast.Node
	child  ast.Node
	text   string
}

func (t *linkTransformer) Transform(node *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	host := URLHost(t.doc.URL)
	var repl []linkNode
	// find all image or link nodes
	ast.Walk(node, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Image:
			repl = append(repl, linkNode{parent: n.Parent(), child: n})
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			link := Link{URL: string(n.Destination), Title: string(n.Text(source))}
			ref := link.Format(len(t.doc.Links), host)
			log.Debugf("%s - %s", ref, link.URL)
			repl = append(repl, linkNode{parent: n.Parent(), child: n, text: ref})
			t.doc.Links = append(t.doc.Links, link)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	// remove images and replace links with formatted text reference
	for _, r := range repl {
		if r.text != "" {
			next := r.child.NextSibling()
			if next != nil && next.Kind() == ast.KindText {
				prev := string(next.Text(source))
				r.parent.ReplaceChild(r.parent, next, ast.NewString([]byte(r.text+prev)))
				r.parent.RemoveChild(r.parent, r.child)
			} else {
				r.parent.ReplaceChild(r.parent, r.child, ast.NewString([]byte(r.text)))
			}
		} else {
			r.parent.RemoveChild(r.parent, r.child)
		}
	}
}

// Render document as markdown formatted text with header and line numbers
func (d *Document) Format(cursor, maxWords int) string {
	log.Debugf("[%d] %s - %s", cursor, d.Title, d.URL)
	lines := len(d.Lines)
	startLine := min(d.StartLine, lines)
	endLine := lines
	if maxWords > 0 {
		words := 0
		for i := startLine; i < endLine; i++ {
			words += len(strings.Fields(d.Lines[i]))
			if words >= maxWords {
				endLine = i
				break
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[%d] %s\n", cursor, d.Title)
	if d.URL != "" {
		fmt.Fprintf(&b, "(%s)\n", d.URL)
	}
	if endLine > startLine {
		fmt.Fprintf(&b, "**viewing lines [%d - %d] of %d**\n\n", startLine+1, endLine, lines)
		for i := startLine; i < endLine; i++ {
			fmt.Fprintf(&b, "L%d: %s\n", i+1, d.Lines[i])
		}
	}
	return b.String()
}

// Find pattern in src as case insensitive substring match - returns line no. if found else -1.
func (d *Document) Find(pattern string) int {
	pattern = strings.ToLower(pattern)
	for i := d.StartLine; i < len(d.Lines); i++ {
		if strings.Contains(strings.ToLower(d.Lines[i]), pattern) {
			return i
		}
	}
	return -1
}

// Append text to document, wrapping text if necessary. Don't wrap links inside 【....】
func (d *Document) Write(text string) {
	prevBlank := false
	for _, line := range strings.Split(text, "\n") {
		blank := strings.TrimSpace(line) == ""
		if prevBlank && blank {
			continue
		}
		prevBlank = blank
		d.Lines = append(d.Lines, "")
		column := 0
		line = html.UnescapeString(line)
		link := false
		for _, r := range line {
			if r == '【' {
				link = true
			} else if r == '】' {
				link = false
			}
			d.Lines[len(d.Lines)-1] += string(r)
			column++
			if d.WrapColumn > 0 && unicode.IsSpace(r) && column >= d.WrapColumn && !link {
				d.Lines = append(d.Lines, "")
				column = 0
			}
		}
	}
}

func incompleteLink(r []rune) bool {
	return slices.Contains(r, '【') && !slices.Contains(r, '】')
}
