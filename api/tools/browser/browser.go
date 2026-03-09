// Package browser implements an OpenAI compatible web browser tool plugin which uses the brave search API and firecrawl scrape API.
package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools"
	"github.com/jnb666/gpt-go/markdown"
	"github.com/jnb666/gpt-go/scrape"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
	log "github.com/sirupsen/logrus"
)

var (
	// Default configuration
	Country            = "gb"
	Language           = "en"
	WrapColumn         = 120
	MaxWords           = 250
	FindMaxWords       = 150
	BraveSearchURL     = "https://api.search.brave.com/res/v1/web/search"
	MaxLinkTitleLength = 100
)

// Current browser state with documents retrieved in this session
type Browser struct {
	Docs        []markdown.Document
	Cursor      int
	BaseID      int
	scaper      scrape.Browser
	braveApiKey string
	nextSearch  time.Time
}

// Create new browser instance
func NewBrowser(braveApiKey string, opts ...func(*scrape.Options)) *Browser {
	return &Browser{
		scaper:      scrape.NewBrowser(opts...),
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
		b.BaseID = 0
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
			log.Warnf("postprocess: error parsing citation %q - invalid cursor %d - expecting 0-%d", ref, cursor, len(s.Docs)-1)
			return ref
		}
		url := s.Docs[cursor].URL
		title := linkTitle(s.Docs[cursor].Title)
		log.Debugf("parse citation %q cursor=%d lines=%s url=%s", ref, cursor, m[2], url)
		return fmt.Sprintf(" [%s†%s](%s %q) ", markdown.URLHost(url), m[2], url, title)
	})
}

func (b *Browser) current() *markdown.Document {
	if b.Cursor >= len(b.Docs) {
		return nil
	}
	return &b.Docs[b.Cursor]
}

func (b *Browser) get(url string) *markdown.Document {
	for i, page := range b.Docs {
		if page.URL == url {
			b.Cursor = i
			return &b.Docs[i]
		}
	}
	return nil
}

func (b *Browser) getLink(id int) (l markdown.Link, ok bool) {
	if id < 0 {
		if doc := b.current(); doc != nil {
			return markdown.Link{URL: doc.URL, Title: doc.Title}, true
		}
	} else {
		for _, page := range b.Docs {
			if id >= page.BaseID && id < page.BaseID+len(page.Links) {
				return page.Links[id-page.BaseID], true
			}
		}
	}
	return
}

func (b *Browser) add(doc markdown.Document) {
	b.Cursor = len(b.Docs)
	b.BaseID += len(doc.Links)
	b.Docs = append(b.Docs, doc)
}

// Tool to search using Brave API - implements api.ToolFunction interface
type Search struct {
	*Browser
	MaxWords int
}

func (t Search) Definition() shared.FunctionDefinitionParam {
	return shared.FunctionDefinitionParam{
		Name: "browser_search",
		Description: openai.String("Searches the web for information related to `query`." +
			" Returns a list of up to 10 links each with the page id, title, url and a brief summary of the page." +
			" Links are formatted as 【{id}†.*】 where id is the page id parameter to pass to the browser_open tool."),
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Text to search for on the web.",
				},
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
	}
	if err := json.Unmarshal([]byte(arg), &args); err != nil {
		return arg, "", err
	}
	req = fmt.Sprintf("browser.search%+v", args)
	if strings.TrimSpace(args.Query) == "" {
		return req, errorResponse(fmt.Errorf("query argument is required")), nil
	}
	url := fmt.Sprintf("https://search.brave.com/search?q=%s&source=web", url.QueryEscape(args.Query))
	if doc := t.get(url); doc != nil {
		log.Debugf("get %s from browser cache", url)
		return req, doc.Format(0), nil
	}
	resp, err := t.search(args.Query, 10)
	if err != nil {
		return req, errorResponse(err), nil
	}
	doc := markdown.Document{
		BaseID:     t.BaseID,
		Title:      fmt.Sprintf("Web search for “%s”", args.Query),
		URL:        url,
		WrapColumn: WrapColumn,
	}
	doc.Write("# Search Results\n\n")
	for i, r := range resp.Web.Results {
		link := markdown.Link{URL: r.URL, Title: r.Title}
		ref := link.Format(t.BaseID, i, "search.brave.com")
		log.Debug(ref)
		doc.Write("  * " + ref + "\n" + r.Description + "\n")
		doc.Links = append(doc.Links, link)
	}
	t.add(doc)
	return req, doc.Format(0), nil
}

type searchResponse struct {
	Web struct {
		Results []struct {
			Title, URL, Description string
		}
	}
	Headers http.Header
}

type ratelimitHeaders struct {
	Limit     [2]int
	Remaining [2]int
	Reset     [2]int
}

// Call Brave web search API
func (t Search) search(query string, topn int) (resp searchResponse, err error) {
	if t.braveApiKey == "" {
		return resp, fmt.Errorf("BraveApiKey is required for search")
	}
	if tm := time.Now(); tm.Before(t.nextSearch) {
		wait := t.nextSearch.Sub(tm)
		log.Infof("Brave search rate limit - wait %s", wait.Round(time.Millisecond))
		time.Sleep(wait)
	}
	uri := fmt.Sprintf("%s?q=%s&count=%d&country=%s&search_lang=%s&text_decorations=false",
		BraveSearchURL, url.QueryEscape(query), topn, Country, Language)
	h, err := tools.Get(uri, &resp, tools.Header{Key: "X-Subscription-Token", Value: t.braveApiKey})
	limits := ratelimitHeaders{
		Limit:     parseHeader(h.Get("X-RateLimit-Limit")),
		Remaining: parseHeader(h.Get("X-RateLimit-Remaining")),
		Reset:     parseHeader(h.Get("X-RateLimit-Reset")),
	}
	log.Debugf("Brave search rate limits: %+v", limits)
	if limits.Remaining[0] == 0 {
		t.nextSearch = time.Now().Add(time.Duration(limits.Reset[0]) * time.Second)
	}
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
		Name: "browser_open",
		Description: openai.String("Opens a web page and returns the text content in Markdown format." +
			" Links in the returned document are replaced with 【{id}†.*】 where id can be passed to a new call to browser_open to go to that page."),
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type": []string{"number", "string"},
					"description": "If `id` is a number it is treated as a page id. If `id` is a string, it is treated as a fully qualified URL." +
						" If not provided then `loc` can be used to scroll the current document.",
				},
				"loc": map[string]any{
					"type":        "number",
					"description": "Line number in the document at which to position the viewport. Defaults to the start of the document if not provided.",
				},
			},
		},
	}
}

// Gets markdown content using a playwright scape request
func (t Open) Call(arg string) (req, res string, err error) {
	log.Infof("[%d] browser_open(%s)", len(t.Docs), arg)
	var args struct {
		ID  any
		Loc float64
	}
	args.Loc = -1
	if err := json.Unmarshal([]byte(arg), &args); err != nil {
		return arg, "", err
	}
	req = fmt.Sprintf("browser.open%+v", args)
	id, url := parseID(args.ID)
	var title string
	log.Debugf("open %+v => id=%d url=%q loc=%g", args, id, url, args.Loc)
	if url == "" {
		if l, ok := t.getLink(id); ok {
			url, title = l.URL, l.Title
		} else {
			return req, errorResponse(fmt.Errorf("page id %d not found", id)), nil
		}
	}
	if doc := t.get(url); doc != nil {
		log.Debugf("get %s from browser cache", url)
		doc.Subtitle = ""
		if args.Loc > 0 {
			doc.StartLine = int(args.Loc)
		}
		return req, doc.Format(0), nil
	}
	doc, err := t.scrape(url, title)
	if err != nil {
		log.Error(err)
		return req, fmt.Sprintf("%s\n(%s)\n", err, doc.URL), nil
	}
	if args.Loc > 0 {
		doc.StartLine = int(args.Loc)
	}
	t.add(doc)
	return req, doc.Format(t.MaxWords), nil
}

// get markdown content for url and extract links from result
func (t Open) scrape(url, title string) (doc markdown.Document, err error) {
	resp, err := t.scaper.Scrape(url)
	if err != nil {
		return doc, err
	}
	if resp.StatusText != "OK" {
		err = fmt.Errorf("error %d: %s", resp.Status, resp.StatusText)
	} else if resp.Title != "" {
		title = resp.Title
	}
	doc = markdown.QuoteLinks(resp.Markdown, url, title, t.BaseID, WrapColumn)
	return doc, err
}

// Tool to find a substring within a retrieved page - implements api.ToolFunction interface
type Find struct {
	*Browser
	MaxWords int
}

func (t Find) Definition() shared.FunctionDefinitionParam {
	return shared.FunctionDefinitionParam{
		Name: "browser_find",
		Description: openai.String("Finds exact matches of `pattern` in the current page, or the page given by `cursor`." +
			" If a match is found then returns the page scrolled to the first line containing that string." +
			" Repeat the same browser_find call to scroll to the next match."),
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
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
	}
	if err := json.Unmarshal([]byte(arg), &args); err != nil {
		return arg, "", err
	}
	req = fmt.Sprintf("browser.find%+v", args)
	if strings.TrimSpace(args.Pattern) == "" {
		return req, errorResponse(fmt.Errorf("pattern argument is required")), nil
	}
	doc := t.current()
	if doc == nil {
		return req, errorResponse(fmt.Errorf("no current document to search")), nil
	}
	if doc.Subtitle != "" {
		// search again in search page
		doc.StartLine++
	}
	line := doc.Find(args.Pattern)
	if line >= 0 {
		doc.Subtitle = fmt.Sprintf("Find results for “%s”", args.Pattern)
		doc.StartLine = line
	} else {
		doc.Subtitle = fmt.Sprintf("“%s” not found", args.Pattern)
		doc.StartLine = len(doc.Lines)
	}
	return req, doc.Format(t.MaxWords), nil
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

func parseID(param any) (id int, url string) {
	switch val := param.(type) {
	case string:
		n, err := strconv.Atoi(val)
		if err == nil {
			id = n
		} else {
			url = val
		}
	case float64:
		id = int(val)
	case int:
		id = val
	default:
		id = -1
	}
	return
}

func parseHeader(h string) (r [2]int) {
	if s1, s2, ok := strings.Cut(h, ","); ok {
		r[0] = atoi(s1)
		r[1] = atoi(s2)
	} else {
		log.Errorf("error parsing header: %s", h)
	}
	return
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		log.Error(err)
	}
	return n
}
