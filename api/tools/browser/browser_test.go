package browser

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSearch(t *testing.T) {
	browser, resp := doSearch(t, "local LLM hosting")
	t.Logf("response:\n%s", resp)
	printLinks(t, browser, 10)
}

func TestOpenURL(t *testing.T) {
	browser := Browser{FirecrawlApiKey: "none"}
	open := Open{Browser: &browser, MaxWords: MaxWords}
	args, _ := json.Marshal(map[string]any{"id": "https://itsabanana.dev/posts/local_llm_hosting-part1/"})
	resp := open.Call(args)
	t.Logf("response:\n%s", resp)
	printLinks(t, browser, 10)
}

func TestOpenID(t *testing.T) {
	browser, _ := doSearch(t, "local LLM hosting")
	open := Open{Browser: &browser, MaxWords: MaxWords}

	args, _ := json.Marshal(map[string]any{"id": 3})
	resp := open.Call(args)
	t.Logf("response:\n%s", resp)

	args, _ = json.Marshal(map[string]any{"loc": 40})
	resp = open.Call(args)
	t.Logf("scroll page:\n%s", resp)

	printLinks(t, browser, 10)
}

func TestFind(t *testing.T) {
	browser, _ := doSearch(t, "local LLM hosting")
	open := Open{Browser: &browser, MaxWords: MaxWords}

	args, _ := json.Marshal(map[string]any{"id": 1})
	resp := open.Call(args)
	t.Logf("open response:\n%s", resp)

	find := Find{Browser: open.Browser, MaxWords: FindMaxWords}
	args, _ = json.Marshal(map[string]any{"pattern": "ollama"})
	for range 3 {
		resp = find.Call(args)
		t.Logf("find response:\n%s", resp)
	}
}

func doSearch(t *testing.T, query string) (Browser, string) {
	browser := Browser{BraveApiKey: os.Getenv("BRAVE_API_KEY"), FirecrawlApiKey: "none"}
	search := Search{Browser: &browser, MaxWords: MaxWords}
	args, _ := json.Marshal(map[string]any{"query": query})
	resp := search.Call(args)
	if len(browser.Docs) != 1 {
		t.Fatal("no document returned")
	}
	return browser, resp
}

func printLinks(t *testing.T, b Browser, num int) {
	if b.Cursor >= len(b.Docs) {
		t.Fatal("no doc retrieved")
	}
	doc := b.Docs[b.Cursor]
	for i, link := range doc.Links {
		if i >= num {
			t.Log("...")
			break
		}
		t.Logf("%d: %s", i, link.URL)
	}
	if len(doc.Links) < num {
		t.Errorf("expecting at least %d links", num)
	}
}
