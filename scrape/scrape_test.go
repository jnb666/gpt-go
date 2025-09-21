package scrape

import (
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

func TestScrape(t *testing.T) {
	urls := []string{
		"https://ollama.com/",
		"https://itsabanana.dev/",
		"https://www.theguardian.com/about",
		"https://ollama.com/",
		"https://www.reuters.com/",
		"https://www.reuters.com/world/uk/",
	}
	referers := map[string]string{
		"https://www.reuters.com/world/uk/": "https://www.reuters.com/",
	}

	b := NewBrowser()
	defer b.Shutdown()
	opts := DefaultOptions

	for _, url := range urls {
		t.Log("scraping", url)
		opts.Referer = referers[url]
		r, err := b.Scrape(url, opts)
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
	b := NewBrowser(false)
	defer b.Shutdown()
	opts := DefaultOptions
	opts.CloseWait = 5 * time.Minute
	b.Scrape("https://bot.sannysoft.com/", opts)
}
