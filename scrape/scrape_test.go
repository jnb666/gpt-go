package scrape

import (
	"errors"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

func scrape(t *testing.T, b Browser, url string) {
	t.Log("scraping", url)
	r, err := b.Scrape(url)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != 200 {
		t.Error("expecting 200 status")
	}
	t.Logf("status: %d %s", r.Status, r.StatusText)
	t.Logf("title: %q", r.Title)
	if log.GetLevel() >= log.DebugLevel {
		t.Logf("content:\n%s\n", r.Markdown)
	}
}

func TestScrape(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	urls := []string{
		"https://ollama.com/",
		"https://itsabanana.dev/",
		"https://www.theguardian.com/about",
		"https://ollama.com/",
	}
	for _, url := range urls {
		scrape(t, b, url)
	}
}

func TestReuters(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	scrape(t, b, "https://www.reuters.com/")
	scrape(t, b, "https://www.reuters.com/world/uk/")
}

func TestReddit(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	scrape(t, b, "https://www.reddit.com/r/LocalLLaMA/comments/1mke7ef/120b_runs_awesome_on_just_8gb_vram/")
}

func TestTelegraph(t *testing.T) {
	opts := func(opt *Options) {
		opt.CDPEndpoint = "http://localhost:9222"
		opt.WaitFor = time.Second
	}
	b := NewBrowser(opts)
	defer b.Shutdown()
	scrape(t, b, "https://www.telegraph.co.uk/business/2026/02/02/labour-backbenchers-revolt-over-starmer-nuclear-plans/")
}

func TestGithub(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	scrape(t, b, "https://github.com/jkup/awesome-personal-blogs")
}

func TestYahoo(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	scrape(t, b, "https://www.yahoo.com/entertainment/")
}

func TestRottenTomatoes(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	scrape(t, b, "https://www.rottentomatoes.com/m/one_battle_after_another")
}

func TestBlogger(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	scrape(t, b, "https://www.retrocomputing.co.uk/")
}

func TestRedirect(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	scrape(t, b, "https://www.retro-computing.com/")
}

func TestInvalidHost(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	_, err := b.Scrape("https://itsabanana.de")
	if e, ok := errors.AsType[*playwright.Error](err); ok {
		t.Log(e)
		if e.Message != "NS_ERROR_UNKNOWN_HOST" {
			t.Error("expecting NS_ERROR_UNKNOWN_HOST")
		}
	} else {
		t.Error("expecting playwright error")
	}
}

func TestNotFound(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	r, err := b.Scrape("https://itsabanana.dev/notfound")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status: %d %s", r.Status, r.StatusText)
	t.Logf("title: %q", r.Title)
	t.Logf("content:\n%s\n", r.Markdown)
	if r.Status != 404 {
		t.Error("expecting 404 status")
	}
}

func TestAntibot(t *testing.T) {
	opts := func(opt *Options) {
		opt.CDPEndpoint = "http://localhost:9222"
		opt.CloseWait = time.Minute
	}
	b := NewBrowser(opts)
	defer b.Shutdown()
	b.Scrape("https://bot.sannysoft.com/")
}
