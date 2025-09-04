// Package browser implements an OpenAI compatible web browser tool plugin which uses the brave search API and firecrawl scrape API.
package browser

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools"
	"github.com/jnb666/gpt-go/markdown"
	"github.com/sashabaranov/go-openai"
	log "github.com/sirupsen/logrus"
)

var (
	// Default configuration
	Timeout            = 5 * time.Second
	Country            = "gb"
	Language           = "en"
	WrapColumn         = 120
	MaxWords           = 300
	FindMaxWords       = 150
	BraveSearchURL     = "https://api.search.brave.com/res/v1/web/search"
	FirecrawlURL       = "http://localhost:3002/v2/scrape"
	MaxCacheAge        = 24 * time.Hour
	MaxLinkTitleLength = 100
)

// Current browser state with documents retrieved in this session
type Browser struct {
	BraveApiKey     string
	FirecrawlApiKey string
	Docs            []markdown.Document
	Cursor          int
}

// Get all defined functions
func (b *Browser) Tools() []api.ToolFunction {
	return []api.ToolFunction{
		&Search{Browser: b, MaxWords: MaxWords},
		&Open{Browser: b, MaxWords: MaxWords},
		&Find{Browser: b, MaxWords: FindMaxWords},
	}
}

// Reset saved document state
func (b *Browser) Reset() {
	b.Cursor = 0
	b.Docs = b.Docs[:0]
}

// Generic description text common to all functions
func (b *Browser) Description() string {
	return "## browser\n\n" +
		"// Tool for browsing.\n" +
		"// The `cursor` appears in brackets before each browsing display: `[{cursor}]`.\n" +
		"// Cite information from the tool using the following format:\n" +
		"// `【{cursor}†L{line_start}(-L{line_end})?】`, for example: `【6†L9-L11】` or `【8†L3】`.\n" +
		"// Do not quote more than 10 words directly from the tool output."
}

var citationRegexp = regexp.MustCompile(`【(\d+)†(L.+?)】`)

// Replace citations in final markdown output with links
func (s *Browser) Postprocess(content string) string {
	return citationRegexp.ReplaceAllStringFunc(content, func(ref string) string {
		m := citationRegexp.FindStringSubmatch(ref)
		if len(m) != 3 {
			log.Warnf("postprocess: citation regex match failed for %q", ref)
			return ref
		}
		cursor, err := strconv.Atoi(m[1])
		if err != nil || cursor < 0 || cursor >= len(s.Docs) {
			log.Warnf("postprocess: error parsing citation %q - invalid cursor", ref)
			return ref
		}
		url := s.Docs[cursor].URL
		title := linkTitle(s.Docs[cursor].Title)
		log.Debugf("parse citation %q cursor=%d lines=%s url=%s", ref, cursor, m[2], url)
		return fmt.Sprintf(" [%s†%s](%s %q) ", markdown.URLHost(url), m[2], url, title)
	})
}

func (b *Browser) current(cursor int) (doc markdown.Document, err error) {
	if cursor >= 0 && cursor < len(b.Docs) {
		return b.Docs[cursor], nil
	}
	if b.Cursor >= 0 && b.Cursor < len(b.Docs) {
		return b.Docs[b.Cursor], nil
	}
	return doc, fmt.Errorf("Document at cursor %d not found", cursor)
}

func (b *Browser) add(doc markdown.Document) {
	b.Cursor = len(b.Docs)
	b.Docs = append(b.Docs, doc)
}

// Tool to search using Brave API - implements api.ToolFunction interface
type Search struct {
	*Browser
	MaxWords int
}

func (t Search) Definition() openai.Tool {
	fn := openai.FunctionDefinition{
		Name:        "browser.search",
		Description: "Searches the web for information related to `query` and displays `topn` results.",
		Parameters: json.RawMessage(`{
	"type": "object",
	"properties": {
		"query": {"type":"string"},
		"topn": {"type":"number", "description":"default: 10"}
	},
	"required": ["query"]
}`)}
	return openai.Tool{Type: openai.ToolTypeFunction, Function: &fn}
}

// Perform a web search add the returned results to the Browser Docs and return markdown formatted text
func (t Search) Call(arg json.RawMessage) string {
	log.Infof("[%d] browser.search(%s)", len(t.Docs), arg)
	var args struct {
		Query string
		TopN  int
	}
	args.TopN = 10
	if err := json.Unmarshal(arg, &args); err != nil {
		return errorResponse(err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return errorResponse(fmt.Errorf("query argument is required"))
	}
	resp, err := t.search(args.Query, args.TopN)
	if err != nil {
		return errorResponse(err)
	}
	doc := markdown.Document{
		Title:      fmt.Sprintf("Web search for “%s”", args.Query),
		URL:        fmt.Sprintf("https://search.brave.com/search?q=%s&source=web", url.QueryEscape(args.Query)),
		WrapColumn: WrapColumn,
	}
	doc.Write("# Search Results\n\n")
	for i, r := range resp.Web.Results {
		link := markdown.Link{URL: r.URL, Title: r.Title}
		ref := link.Format(i, "")
		log.Debugf("%s %s", ref, r.URL)
		doc.Write("  * " + ref + "\n" + r.Description + "\n")
		doc.Links = append(doc.Links, link)
	}
	t.add(doc)
	return doc.Format(t.Cursor, t.MaxWords)
}

type searchResponse struct {
	Web struct {
		Results []struct {
			Title, URL, Description string
		}
	}
}

// Call Brave web search API
func (t Search) search(query string, topn int) (resp searchResponse, err error) {
	if t.BraveApiKey == "" {
		return resp, fmt.Errorf("BraveApiKey is required for search")
	}
	uri := fmt.Sprintf("%s?q=%s&count=%d&country=%s&search_lang=%s&text_decorations=false",
		BraveSearchURL, url.QueryEscape(query), topn, Country, Language)
	err = tools.Get(uri, &resp, tools.Header{Key: "X-Subscription-Token", Value: t.BraveApiKey})
	if err != nil {
		return resp, err
	}
	if len(resp.Web.Results) == 0 {
		return resp, fmt.Errorf("no search results returned")
	}
	return resp, nil
}

// Tool to fetch a web URL using Firecrawl - implements api.ToolFunction interface
type Open struct {
	*Browser
	MaxWords int
}

func (t Open) Definition() openai.Tool {
	fn := openai.FunctionDefinition{
		Name: "browser.open",
		Description: "Opens the link `id` from the page indicated by `cursor` starting at line number `loc`.\n" +
			"Valid link ids are displayed with the formatting: `【{id}†.*】`.\n" +
			"If `cursor` is not provided, the most recent page is implied.\n" +
			"If `id` is a string, it is treated as a fully qualified URL.\n" +
			"If `loc` is not provided, the viewport will be positioned at the beginning of the document.\n" +
			"Use this function without `id` to scroll to a new location of an opened page.",
		Parameters: json.RawMessage(`{
	"type": "object",
	"properties": {
		"cursor": {"type": "number", "description":"default: -1"},
		"id": {"type":["number","string"], "description":"default: -1"},
		"loc": {"type":"number", "description":"default: -1"}
	}
}`)}
	return openai.Tool{Type: openai.ToolTypeFunction, Function: &fn}
}

// Gets markdown content using a Firecrawl scape request
func (t Open) Call(arg json.RawMessage) string {
	log.Infof("[%d] browser.open(%s)", len(t.Docs), arg)
	var args struct {
		Cursor int
		ID     any
		Loc    int
	}
	args.Cursor = -1
	args.Loc = -1
	if err := json.Unmarshal(arg, &args); err != nil {
		return errorResponse(err)
	}
	var current, doc markdown.Document
	var err error
	switch url := args.ID.(type) {
	case string:
		doc, err = t.scrape(url, "")
	case float64:
		id := int(url)
		if current, err = t.current(args.Cursor); err == nil {
			if id >= 0 && id < len(current.Links) {
				doc, err = t.scrape(current.Links[id].URL, current.Links[id].Title)
			} else {
				doc = current
			}
		}
	default:
		doc, err = t.current(args.Cursor)
	}
	if err != nil {
		return errorResponse(err)
	}
	if args.Loc >= 0 {
		doc.StartLine = args.Loc
	}
	t.add(doc)
	return doc.Format(t.Cursor, t.MaxWords)
}

type scrapeResponse struct {
	Success bool
	Data    struct {
		Markdown string
		RawHTML  string
		Metadata struct {
			Title      string
			StatusCode int
		}
	}
}

// get markdown content for url and extract links from result
func (t Open) scrape(url, title string) (doc markdown.Document, err error) {
	request := map[string]any{
		"url":     url,
		"formats": []string{"markdown", "rawHtml"},
		"maxAge":  MaxCacheAge.Milliseconds(),
		"timeout": Timeout.Milliseconds(),
	}
	var reply scrapeResponse
	log.Info("    ", url)
	err = tools.Post(FirecrawlURL, request, &reply, tools.Header{Key: "Authorization", Value: "Bearer " + t.FirecrawlApiKey})
	if err != nil {
		return doc, err
	}
	if reply.Data.Metadata.Title != "" {
		title = reply.Data.Metadata.Title
	} else {
		title = htmlTitle(reply.Data.RawHTML, title, url)
	}
	if !reply.Success {
		return doc, fmt.Errorf("error retrieving page: %s", title)
	}
	doc = markdown.QuoteLinks(reply.Data.Markdown, url, title, WrapColumn)
	return doc, err
}

// Tool to find a substring within a retrieved page - implements api.ToolFunction interface
type Find struct {
	*Browser
	MaxWords int
}

func (t Find) Definition() openai.Tool {
	fn := openai.FunctionDefinition{
		Name:        "browser.find",
		Description: "Finds exact matches of `pattern` in the current page, or the page given by `cursor`.",
		Parameters: json.RawMessage(`{
	"type": "object",
	"properties": {
		"pattern": {"type":"string"},
		"cursor": {"type":"number", "description":"default: -1"}
	},
	"required": ["pattern"]
}`)}
	return openai.Tool{Type: openai.ToolTypeFunction, Function: &fn}
}

var reFind = regexp.MustCompile(`^Find results for “(.+?)” in “(.+?)”`)

func (t Find) Call(arg json.RawMessage) string {
	log.Infof("[%d] browser.find(%s)", len(t.Docs), arg)
	// parse arguments
	var args struct {
		Pattern string
		Cursor  int
	}
	args.Cursor = -1
	if err := json.Unmarshal(arg, &args); err != nil {
		return errorResponse(err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return errorResponse(fmt.Errorf("pattern argument is required"))
	}
	current, err := t.current(args.Cursor)
	if err != nil {
		return errorResponse(err)
	}
	if m := reFind.FindStringSubmatch(current.Title); len(m) > 0 {
		// search again in search page
		current.Title = m[2]
		if m[1] == args.Pattern {
			current.StartLine++
		}
	}
	line := current.Find(args.Pattern)
	doc := current
	if line >= 0 {
		doc.Title = fmt.Sprintf("Find results for “%s” in “%s”", args.Pattern, current.Title)
		doc.StartLine = line
	} else {
		doc.Title = fmt.Sprintf("“%s” not found in page “%s”", args.Pattern, current.Title)
		doc.StartLine = len(doc.Lines)
	}
	t.add(doc)
	return doc.Format(t.Cursor, t.MaxWords)
}

func errorResponse(err error) string {
	log.Error(err)
	return fmt.Sprintf("Error: %s", err)
}

var titleRegexp = regexp.MustCompile(`(?i)<title>(.+?)</title>`)

func htmlTitle(doc, title, url string) string {
	m := titleRegexp.FindStringSubmatch(doc)
	if len(m) >= 2 {
		return html.UnescapeString(m[1])
	}
	if title != "" {
		return title
	}
	return url
}

func linkTitle(s string) string {
	if len(s) > MaxLinkTitleLength {
		s = s[:MaxLinkTitleLength] + "…"
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		return ' '
	}, s)
	return s
}
