// Package browser implements an OpenAI compatible web browser tool plugin which uses the brave search API and firecrawl scrape API.
package browser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools"
	"github.com/jnb666/gpt-go/markdown"
	"github.com/jnb666/gpt-go/scrape"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/shared"
	log "github.com/sirupsen/logrus"
)

var (
	// Default configuration
	Country            = "gb"
	Language           = "en"
	WrapColumn         = 120
	MaxWords           = 300
	FindMaxWords       = 150
	BraveSearchURL     = "https://api.search.brave.com/res/v1/web/search"
	MaxLinkTitleLength = 100
)

// Current browser state with documents retrieved in this session
type Browser struct {
	Docs        []markdown.Document
	Cursor      int
	scaper      scrape.Browser
	braveApiKey string
}

// Create new browser instance
func NewBrowser(braveApiKey string) *Browser {
	return &Browser{
		scaper:      scrape.NewBrowser(),
		braveApiKey: braveApiKey,
	}
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
	if b != nil {
		b.Cursor = 0
		b.Docs = b.Docs[:0]
	}
}

// Close browser and release all resources
func (b *Browser) Close() {
	if b != nil {
		b.scaper.Shutdown()
	}
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
	return doc, fmt.Errorf("document at cursor %d not found", cursor)
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

func (t Search) Definition() shared.FunctionDefinitionParam {
	return shared.FunctionDefinitionParam{
		Name:   "browser_search",
		Strict: openai.Bool(true),
		Description: openai.String("Searches the web for information related to `query` and displays `topn` results." +
			" The `cursor` appears in brackets before each browsing display: `[{cursor}]`." +
			" Cite information from the tool using the following format:【{cursor}†L{line_start}(-L{line_end})?】for example: `【6†L9-L11】` or `【8†L3】`."),
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Text to search for on the web."},
				"topn":  map[string]any{"type": "number", "description": "Maximum number of results to return - default 10."},
			},
			"required": []string{"query"},
		},
	}
}

// Perform a web search add the returned results to the Browser Docs and return markdown formatted text
func (t Search) Call(arg string) (req, res string, err error) {
	log.Infof("[%d] browser_search(%s)", len(t.Docs), arg)
	var args struct {
		Query string
		TopN  float64
	}
	args.TopN = 10
	if err := json.Unmarshal([]byte(arg), &args); err != nil {
		return arg, "", err
	}
	req = fmt.Sprintf("browser.search%+v", args)
	if strings.TrimSpace(args.Query) == "" {
		return req, errorResponse(fmt.Errorf("query argument is required")), nil
	}
	resp, err := t.search(args.Query, int(args.TopN))
	if err != nil {
		return req, errorResponse(err), nil
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
	return req, doc.Format(t.Cursor, t.MaxWords), nil
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
	if t.braveApiKey == "" {
		return resp, fmt.Errorf("BraveApiKey is required for search")
	}
	uri := fmt.Sprintf("%s?q=%s&count=%d&country=%s&search_lang=%s&text_decorations=false",
		BraveSearchURL, url.QueryEscape(query), topn, Country, Language)
	err = tools.Get(uri, &resp, tools.Header{Key: "X-Subscription-Token", Value: t.braveApiKey})
	if err != nil {
		return resp, err
	}
	if len(resp.Web.Results) == 0 {
		return resp, fmt.Errorf("no search results returned")
	}
	return resp, nil
}

// Tool to fetch a web URL using scrape module - implements api.ToolFunction interface
type Open struct {
	*Browser
	MaxWords int
}

func (t Open) Definition() shared.FunctionDefinitionParam {
	return shared.FunctionDefinitionParam{
		Name:   "browser_open",
		Strict: openai.Bool(true),
		Description: openai.String("Opens the link `id` from the page indicated by `cursor` starting at line number `loc`." +
			" The `cursor` appears in brackets before each browsing display: `[{cursor}]`." +
			" Cite information from the tool using the following format:【{cursor}†L{line_start}(-L{line_end})?】for example: `【6†L9-L11】` or `【8†L3】`."),
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"cursor": map[string]any{
					"type":        "number",
					"description": "Identifies the current page. If not provided the most recent page is implied.",
				},
				"id": map[string]any{
					"type": []string{"number", "string"},
					"description": "If `id` is a number it is treated as a link id from the page given by `cursor`. Valid link ids are displayed with the formatting: `【{id}†.*】.`" +
						"  If `id` is a string, it is treated as a fully qualified URL." +
						"  Use this function without `id` to scroll to a new location of an opened page.",
				},
				"loc": map[string]any{
					"type":        "number",
					"description": "Line number in the document at which to position the viewport - defaults to the start of the documnet if not provided."},
			},
		},
	}
}

// Gets markdown content using a Firecrawl scape request
func (t Open) Call(arg string) (req, res string, err error) {
	log.Infof("[%d] browser_open(%s)", len(t.Docs), arg)
	var args struct {
		Cursor float64
		ID     any
		Loc    float64
	}
	args.Cursor = -1
	args.Loc = -1
	if err := json.Unmarshal([]byte(arg), &args); err != nil {
		return arg, "", err
	}
	req = fmt.Sprintf("browser.open%+v", args)
	var current, doc markdown.Document
	switch url := args.ID.(type) {
	case string:
		doc, err = t.scrape(url, "", "")
	case float64:
		id := int(url)
		if current, err = t.current(int(args.Cursor)); err == nil {
			if id >= 0 && id < len(current.Links) {
				doc, err = t.scrape(current.Links[id].URL, current.Links[id].Title, current.URL)
			} else {
				doc = current
			}
		}
	default:
		doc, err = t.current(int(args.Cursor))
	}
	if err != nil {
		log.Error(err)
		return req, fmt.Sprintf("%s\n(%s)\n", err, doc.URL), nil
	}
	if args.Loc >= 0 {
		doc.StartLine = int(args.Loc)
	}
	t.add(doc)
	return req, doc.Format(t.Cursor, t.MaxWords), nil
}

// get markdown content for url and extract links from result
func (t Open) scrape(url, title, referer string) (doc markdown.Document, err error) {
	doc.URL = url
	doc.Title = title
	resp, err := t.scaper.Scrape(url) //, func(opt *scrape.Options) { opt.Referer = referer })
	if err != nil {
		return doc, err
	}
	if resp.StatusText != "OK" {
		err = fmt.Errorf("error %d: %s", resp.Status, resp.StatusText)
	} else if resp.Title != "" {
		title = resp.Title
	}
	doc = markdown.QuoteLinks(resp.Markdown, url, title, WrapColumn)
	return doc, err
}

// Tool to find a substring within a retrieved page - implements api.ToolFunction interface
type Find struct {
	*Browser
	MaxWords int
}

func (t Find) Definition() shared.FunctionDefinitionParam {
	return shared.FunctionDefinitionParam{
		Name:   "browser_find",
		Strict: openai.Bool(true),
		Description: openai.String("Finds exact matches of `pattern` in the current page, or the page given by `cursor`." +
			" The `cursor` appears in brackets before each browsing display: `[{cursor}]`." +
			" Cite information from the tool using the following format:【{cursor}†L{line_start}(-L{line_end})?】for example: `【6†L9-L11】` or `【8†L3】`."),
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"cursor": map[string]any{
					"type":        "number",
					"description": "Identifies the current page. If not provided the most recent page is implied.",
				},
				"pattern": map[string]any{
					"type":        "string",
					"description": "Text to search for.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

var reFind = regexp.MustCompile(`^Find results for “(.+?)” in “(.+?)”`)

func (t Find) Call(arg string) (req, res string, err error) {
	log.Infof("[%d] browser_find(%s)", len(t.Docs), arg)
	// parse arguments
	var args struct {
		Pattern string
		Cursor  float64
	}
	args.Cursor = -1
	if err := json.Unmarshal([]byte(arg), &args); err != nil {
		return arg, "", err
	}
	req = fmt.Sprintf("browser.find%+v", args)
	if strings.TrimSpace(args.Pattern) == "" {
		return req, errorResponse(fmt.Errorf("pattern argument is required")), nil
	}
	current, err := t.current(int(args.Cursor))
	if err != nil {
		return req, errorResponse(err), nil
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
	return req, doc.Format(t.Cursor, t.MaxWords), nil
}

func errorResponse(err error) string {
	log.Error(err)
	return fmt.Sprintf("Error: %s", err)
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
