package scrape

import (
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

func scrape(t *testing.T, b Browser, url string, withOptions ...func(*Options)) {
	t.Log("scraping", url)
	r, err := b.Scrape(url, withOptions...)
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
	scrape(t, b, "https://www.reuters.com/world/uk/",
		func(opt *Options) { opt.Referer = "https://www.reuters.com/" })
}

func TestReddit(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	scrape(t, b, "https://www.reddit.com/r/LocalLLaMA/comments/1mke7ef/120b_runs_awesome_on_just_8gb_vram/")
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

func TestInvalidHost(t *testing.T) {
	b := NewBrowser()
	defer b.Shutdown()
	_, err := b.Scrape("https://itsabanana.de")
	t.Log(err)
	if err.Error() != "Error getting page: NS_ERROR_UNKNOWN_HOST" {
		t.Error("expecting error")
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
	b := NewBrowser(func(opt *Options) {
		opt.Headless = false
		opt.CloseWait = time.Minute
	})
	defer b.Shutdown()
	b.Scrape("https://bot.sannysoft.com/")
}
