package browser

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSearch(t *testing.T) {
	browser := newBrowser()
	defer browser.Close()
	resp := doSearch(t, browser, "local LLM hosting")
	t.Logf("response:\n%s", resp)
	printLinks(t, browser, 10)
}

func TestOpenURL(t *testing.T) {
	browser := newBrowser()
	defer browser.Close()
	open := Open{Browser: browser, MaxWords: MaxWords}
	_, resp, _ := open.Call(marshal(map[string]any{"id": "https://itsabanana.dev/posts/local_llm_hosting-part1/"}))
	t.Logf("response:\n%s", resp)
	printLinks(t, browser, 10)
}

func TestOpenWikipedia(t *testing.T) {
	browser := newBrowser()
	defer browser.Close()
	open := Open{Browser: browser, MaxWords: MaxWords}
	_, resp, _ := open.Call(marshal(map[string]any{"id": "https://en.wikipedia.org/wiki/Liz_Truss"}))
	t.Logf("response:\n%s", resp)
	printLinks(t, browser, 10)
}

func TestNotFound(t *testing.T) {
	browser := newBrowser()
	defer browser.Close()
	open := Open{Browser: browser, MaxWords: MaxWords}
	_, resp, _ := open.Call(marshal(map[string]any{"id": "https://itsabanana.dev/nonsuch/"}))
	t.Logf("response:\n%s", resp)
	if !strings.HasPrefix(resp, "Error 404: Not Found") {
		t.Error("expecting error")
	}
}

func TestBlocked(t *testing.T) {
	browser := newBrowser()
	defer browser.Close()
	open := Open{Browser: browser, MaxWords: MaxWords}
	_, resp, _ := open.Call(marshal(map[string]any{"id": "https://www.g2.com/"}))
	t.Logf("response:\n%s", resp)
	if !strings.HasPrefix(resp, "Error 403: Forbidden") {
		t.Error("expecting error")
	}
}

func TestOpenID(t *testing.T) {
	browser := newBrowser()
	defer browser.Close()
	doSearch(t, browser, "local LLM hosting")

	open := Open{Browser: browser, MaxWords: MaxWords}
	_, resp, _ := open.Call(marshal(map[string]any{"id": 3}))
	t.Logf("response:\n%s", resp)
	_, resp, _ = open.Call(marshal(map[string]any{"loc": 63}))
	t.Logf("scroll page:\n%s", resp)

	printLinks(t, browser, 10)
}

func TestFind(t *testing.T) {
	browser := newBrowser()
	defer browser.Close()

	open := Open{Browser: browser, MaxWords: MaxWords}
	_, resp, _ := open.Call(marshal(map[string]any{"id": "https://blog.n8n.io/local-llm/"}))
	t.Logf("open response:\n%s", resp)

	find := Find{Browser: open.Browser, MaxWords: FindMaxWords}
	for range 3 {
		_, resp, _ = find.Call(marshal(map[string]any{"pattern": "video ram"}))
		t.Logf("find response:\n%s", resp)
	}
	printLinks(t, browser, 10)
}

func newBrowser() *Browser {
	return NewBrowser(os.Getenv("BRAVE_API_KEY"))
}

func doSearch(t *testing.T, browser *Browser, query string) string {
	search := Search{Browser: browser, MaxWords: MaxWords}
	_, resp, err := search.Call(marshal(map[string]any{"query": query}))
	if err != nil {
		t.Fatal(err)
	}
	if len(browser.Docs) != 1 {
		t.Fatal("no document returned")
	}
	return resp
}

func printLinks(t *testing.T, b *Browser, num int) {
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

func marshal(args any) string {
	data, _ := json.Marshal(args)
	return string(data)
}
