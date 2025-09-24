// Package scrape provides a web page scraper using Playwright.
package scrape

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/PuerkitoBio/goquery"
	"github.com/playwright-community/playwright-go"
	log "github.com/sirupsen/logrus"
)

var (
	DefaultOptions = Options{
		Timeout:           15 * time.Second,
		MaxAge:            8 * time.Hour,
		MaxSpeed:          time.Second,
		WaitUntil:         playwright.WaitUntilStateLoad,
		Locale:            "en-GB",
		Timezone:          "Europe/London",
		IgnoreHttpsErrors: true,
	}
)

type Options struct {
	Timeout           time.Duration // Timeout for each goto request
	MaxAge            time.Duration // Used cached response if age of request less than this
	MaxSpeed          time.Duration // Minimum delay between requests from same host
	CloseWait         time.Duration // Wait before closing context
	WaitUntil         *playwright.WaitUntilState
	Referer           string
	Locale            string
	Timezone          string
	IgnoreHttpsErrors bool
}

// Browser instance
type Browser struct {
	playwright *playwright.Playwright
	browser    playwright.Browser
	cache      map[string]Response
}

// Load new firefox browser in headless mode by default, will panic on error
func NewBrowser(headless ...bool) Browser {
	log.Info("scrape: new browser")
	pw, err := playwright.Run()
	if err != nil {
		panic(err)
	}
	if len(headless) == 0 {
		headless = []bool{true}
	}
	br, err := pw.Firefox.Launch(playwright.BrowserTypeLaunchOptions{Headless: &headless[0]})
	if err != nil {
		panic(err)
	}
	return Browser{playwright: pw, browser: br, cache: map[string]Response{}}
}

// Close browser and stop playwright
func (b Browser) Shutdown() {
	log.Info("scrape: shutdown browser")
	b.browser.Close()
	b.playwright.Stop()
}

// Scrape page response data
type Response struct {
	URL        string
	Title      string
	Markdown   string
	RawHTML    string
	Status     int
	StatusText string
	Timestamp  time.Time
}

// Scrape HTML content from given URL and convert to Markdown. referer is optional. Uses DefaultOptions if opts not specified.
func (b Browser) Scrape(uri string, opts ...Options) (r Response, err error) {
	opt := DefaultOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if r, ok := b.cache[uri]; ok && r.Status == 200 && time.Since(r.Timestamp) < opt.MaxAge {
		log.Debugf("scrape: get %s from cache", uri)
		return r, nil
	}
	log.Info("scrape: ", uri)
	if opt.MaxSpeed > 0 {
		b.delay(uri, opt.MaxSpeed)
	}
	r, err = b.scrape(uri, opt)
	if err != nil {
		e := new(playwright.Error)
		if errors.As(err, &e) {
			return r, fmt.Errorf("error getting page: %s", e.Message)
		}
		return r, err
	}
	r, err = toMarkdown(r)
	if err != nil {
		return r, err
	}
	b.cache[uri] = r
	return r, nil
}

func (b Browser) scrape(uri string, opt Options) (r Response, err error) {
	ctx, err := b.browser.NewContext(playwright.BrowserNewContextOptions{
		IgnoreHttpsErrors: &opt.IgnoreHttpsErrors,
		Locale:            &opt.Locale,
		TimezoneId:        &opt.Timezone,
		Viewport:          &playwright.Size{Width: 1280 + rand.IntN(400), Height: 720 + rand.IntN(200)},
	})
	defer func() {
		time.Sleep(opt.CloseWait)
		ctx.Close()
	}()

	page, err := ctx.NewPage()
	if err != nil {
		return r, err
	}
	page.AddInitScript(playwright.Script{Content: &stealthJS})

	page.Route("**/*", func(r playwright.Route) {
		uri := r.Request().URL()
		ext := path.Ext(uri)
		if ext != "" && slices.Contains(mediaExtensions, ext[1:]) {
			log.Trace("skip extension ", uri)
			r.Abort()
			return
		}
		u, err := url.Parse(uri)
		if err == nil && slices.Contains(addServingDomains, u.Hostname()) {
			log.Trace("skip domain ", uri)
			r.Abort()
			return
		}
		log.Trace("get ", uri)
		r.Continue()
	})

	gotoOpts := playwright.PageGotoOptions{WaitUntil: opt.WaitUntil}
	if opt.Timeout > 0 {
		timeout := float64(opt.Timeout.Milliseconds())
		gotoOpts.Timeout = &timeout
	}
	if opt.Referer != "" {
		gotoOpts.Referer = &opt.Referer
	}
	resp, err := page.Goto(uri, gotoOpts)
	if err != nil {
		return r, err
	}
	r.URL = uri
	r.Timestamp = time.Now()
	r.RawHTML, err = page.Content()
	if err != nil {
		return r, err
	}
	r.Status = resp.Status()
	if text, ok := statusCodes[r.Status]; ok {
		r.StatusText = text
	} else if resp.Ok() {
		r.StatusText = "OK"
	}
	if resp.StatusText() != "" {
		r.StatusText = resp.StatusText()
	}
	r.Title, err = page.Title()
	return r, err
}

func (b Browser) delay(uri string, maxSpeed time.Duration) {
	host := getHost(uri)
	latest := 24 * time.Hour
	for key, val := range b.cache {
		if getHost(key) == host {
			latest = min(latest, time.Since(val.Timestamp))
		}
	}
	if latest < maxSpeed {
		d := maxSpeed - latest
		log.Debugf("scrape: sleep for %s", d.Round(time.Millisecond))
		time.Sleep(d)
	}
}

var reStrip = regexp.MustCompile(`(?m)\s*<!--THE END-->\n*`)

func toMarkdown(r Response) (Response, error) {
	// filter tags
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(r.RawHTML))
	if err != nil {
		return r, err
	}
	for _, tag := range tagsToRemove {
		doc.Find(tag).Each(func(n int, s *goquery.Selection) {
			node := s.Nodes[0]
			if tag[0] != '.' && tag[0] != '#' || !slices.Contains(tagsToKeep, node.Data) {
				log.Debugf("remove %s %s", node.Data, tag)
				s.Remove()
			}
		})
	}
	html, err := doc.Html()
	if err != nil {
		return r, err
	}
	// convert to markdown
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
		),
	)
	r.Markdown, err = conv.ConvertString(html, converter.WithDomain(r.URL))
	r.Markdown = reStrip.ReplaceAllLiteralString(r.Markdown, "")
	return r, err
}

func getHost(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		log.Error(err)
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
