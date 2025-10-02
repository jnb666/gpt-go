// Package scrape provides a web page scraper using Playwright.
package scrape

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
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

type Options struct {
	Timeout           time.Duration // Timeout for each goto request
	MaxAge            time.Duration // Used cached response if age of request less than this
	MaxSpeed          time.Duration // Minimum delay between requests from same host
	CloseWait         time.Duration // Wait before closing context
	WaitFor           time.Duration // Wait after load has completed
	WaitUntil         playwright.WaitUntilState
	Referer           string
	Locale            string
	Timezone          string
	IgnoreHttpsErrors bool
	Headless          bool
	WithExtension     string
}

func DefaultOptions(uri string) Options {
	opts := Options{
		Timeout:           15 * time.Second,
		MaxAge:            8 * time.Hour,
		MaxSpeed:          time.Second,
		WaitUntil:         *playwright.WaitUntilStateLoad,
		Locale:            "en-GB",
		Timezone:          "Europe/London",
		IgnoreHttpsErrors: true,
		Headless:          true,
	}
	if uri != "" {
		host := getHost(uri)
		for _, domain := range cookieAddonDomains {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				opts.WithExtension = "isdcac"
				opts.WaitFor = cookieWaitDefault
			}
		}
		for _, domain := range waitDomains {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				opts.WaitFor = waitDefault
			}
		}
	}

	return opts
}

// Browser instance
type Browser struct {
	playwright *playwright.Playwright
	browser    playwright.Browser
	cache      map[string]Response
}

// Load new firefox browser. If withOptions is set it can be used to override DefaultOptions. Will panic on error.
func NewBrowser(withOptions ...func(*Options)) Browser {
	log.Info("scrape: new browser")
	var err error
	opt := DefaultOptions("")
	b := Browser{cache: map[string]Response{}}
	if len(withOptions) > 0 && withOptions[0] != nil {
		withOptions[0](&opt)
	}
	b.playwright, err = playwright.Run()
	if err != nil {
		panic(err)
	}
	b.browser, err = b.playwright.Firefox.Launch(playwright.BrowserTypeLaunchOptions{Headless: &opt.Headless})
	if err != nil {
		panic(err)
	}
	return b
}

// Close browser and stop playwright
func (b Browser) Shutdown() {
	log.Info("scrape: shutdown browser")
	if b.browser != nil {
		b.browser.Close()
	}
	b.playwright.Stop()
}

// Scrape page response data
type Response struct {
	URL        string
	Title      string
	Markdown   string
	RawHTML    string
	MainHTML   string
	Status     int
	StatusText string
	Timestamp  time.Time
}

// Scrape HTML content from given URL and convert to Markdown. referer is optional. If withOptions is specified it can be used to override the default options.
func (b Browser) Scrape(uri string, withOptions ...func(*Options)) (r Response, err error) {
	opt := DefaultOptions(uri)
	if len(withOptions) > 0 && withOptions[0] != nil {
		withOptions[0](&opt)
	}
	log.Debugf("scrape options: %+v", opt)
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
	var ctx playwright.BrowserContext
	viewport := playwright.Size{Width: 1280 + rand.IntN(400), Height: 720 + rand.IntN(200)}
	if opt.WithExtension == "" {
		ctx, err = b.browser.NewContext(
			playwright.BrowserNewContextOptions{
				IgnoreHttpsErrors: &opt.IgnoreHttpsErrors,
				Locale:            &opt.Locale,
				TimezoneId:        &opt.Timezone,
				Viewport:          &viewport,
			})
	} else {
		channel := "chromium"
		var userDataDir string
		userDataDir, err = os.MkdirTemp("", "chromium_user_data")
		if err != nil {
			return r, err
		}
		defer os.RemoveAll(userDataDir)
		extension := opt.WithExtension
		if !filepath.IsAbs(extension) {
			extension = filepath.Join(extensionDir(), extension)
		}
		log.Debug("loading extension from ", extension)
		ctx, err = b.playwright.Chromium.LaunchPersistentContext(userDataDir,
			playwright.BrowserTypeLaunchPersistentContextOptions{
				Headless:          &opt.Headless,
				Channel:           &channel,
				Args:              []string{"--disable-extensions-except=" + extension, "--load-extension=" + extension},
				IgnoreHttpsErrors: &opt.IgnoreHttpsErrors,
				Locale:            &opt.Locale,
				TimezoneId:        &opt.Timezone,
				Viewport:          &viewport,
			})
	}
	if err != nil {
		return r, fmt.Errorf("new browser context error: %w", err)
	}
	defer func() {
		time.Sleep(opt.CloseWait)
		ctx.Close()
	}()

	page, err := ctx.NewPage()
	if err != nil {
		return r, fmt.Errorf("new page error: %w", err)
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

	gotoOpts := playwright.PageGotoOptions{WaitUntil: &opt.WaitUntil}
	if opt.Timeout > 0 {
		timeout := float64(opt.Timeout.Milliseconds())
		gotoOpts.Timeout = &timeout
	}
	if opt.Referer != "" {
		gotoOpts.Referer = &opt.Referer
	}
	resp, err := page.Goto(uri, gotoOpts)
	if err != nil {
		return r, fmt.Errorf("page goto error: %w", err)
	}
	r.Timestamp = time.Now()
	r.URL = uri
	r.Status = resp.Status()
	if text, ok := statusCodes[r.Status]; ok {
		r.StatusText = text
	} else if resp.Ok() {
		r.StatusText = "OK"
	}
	r.RawHTML, r.Title, err = getContent(page, opt)
	if err != nil {
		err = fmt.Errorf("get page content error: %w", err)
	}
	return r, err
}

func getContent(page playwright.Page, opt Options) (content, title string, err error) {
	for {
		n, _ := page.Locator("meta[http-equiv='refresh']").Count()
		if n == 0 {
			break
		}
		log.Debugf("%s: got meta refresh - waiting", page.URL())
		page.WaitForTimeout(100)
	}
	for n := 0; n < maxRetries; n++ {
		if opt.WaitFor > 0 {
			page.WaitForTimeout(float64(opt.WaitFor.Milliseconds()))
		}
		content, err = page.Content()
		if err == nil || opt.WaitFor == 0 {
			break
		}
		log.Warn(err)
	}
	if err != nil {
		return
	}
	if removed, err := page.Evaluate(removeHiddenJS); err == nil {
		content = removed.(string)
	} else {
		log.Warn(err)
	}
	title, err = page.Title()
	return
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
	r.MainHTML, err = doc.Html()
	if err != nil {
		return r, err
	}
	r.MainHTML = strings.ReplaceAll(r.MainHTML, "\u00a0", " ") // &nbsp; elements
	// convert to markdown
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
		),
	)
	r.Markdown, err = conv.ConvertString(r.MainHTML, converter.WithDomain(r.URL))
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

func extensionDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "extensions")
}
