// Interactive test harness for browser tool
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/scanner"
	"time"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/browser"
	"github.com/jnb666/gpt-go/scrape"
	log "github.com/sirupsen/logrus"
)

func main() {
	var debug bool
	var cdpEndpoint string
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.StringVar(&cdpEndpoint, "cdp", "", "connect to browser at this chrome dev tools endpoint if set")
	flag.Parse()

	if debug {
		log.SetLevel(log.DebugLevel)
	}

	apiKey := os.Getenv("BRAVE_API_KEY")
	if apiKey == "" {
		log.Fatal("BRAVE_API_KEY environment variable must be set")
	}

	var opts func(*scrape.Options)
	if cdpEndpoint != "" {
		opts = func(o *scrape.Options) {
			o.CDPEndpoint = cdpEndpoint
			o.WaitFor = 250 * time.Millisecond
		}
	}
	browse := browser.NewBrowser(apiKey, opts)
	defer browse.Close()

	tools := map[string]api.ToolFunction{}
	fmt.Println("# Tools\n\nYou have access to the following functions:\n\n<tools>")
	for _, tool := range browse.Tools() {
		fmt.Println(toJSON(tool.Definition()))
		tools[tool.Definition().Name] = tool
	}
	fmt.Println("</tools>")

	input := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		cmd, err := input.ReadString('\n')
		if err != nil {
			break
		}
		flds, err := parseLine(cmd)
		if err != nil {
			log.Error(err)
			continue
		}
		if len(flds) == 0 {
			continue
		}
		log.Debugf("%#v", flds)
		var resp string
		switch strings.ToLower(flds[0].txt) {
		case "browser_search", "search":
			_, resp, err = tools["browser_search"].Call(params(flds[1:], "query", "topn"))
		case "browser_open", "open":
			_, resp, err = tools["browser_open"].Call(params(flds[1:], "id", "loc"))
		case "browser_find", "find":
			_, resp, err = tools["browser_find"].Call(params(flds[1:], "pattern"))
		default:
			log.Error("unknown command: ", flds[0])
			continue
		}
		if err == nil {
			fmt.Println(resp)
		} else {
			log.Error(err)
		}
	}
}

type Token struct {
	typ rune
	txt string
}

func parseLine(text string) (toks []Token, err error) {
	var s scanner.Scanner
	s.Init(strings.NewReader(text))
	s.Mode = scanner.ScanInts | scanner.ScanStrings | scanner.ScanIdents
	minus := ""
	for t := s.Scan(); t != scanner.EOF; t = s.Scan() {
		switch t {
		case '-':
			minus = "-"
		case scanner.Int, scanner.String, scanner.Ident:
			if minus != "" && t != scanner.Int {
				return nil, fmt.Errorf("invalid token: %c", t)
			}
			toks = append(toks, Token{typ: t, txt: minus + s.TokenText()})
			minus = ""
		default:
			return nil, fmt.Errorf("invalid token: %c", t)
		}
	}
	return toks, nil
}

func params(flds []Token, names ...string) string {
	var args []string
	for i := range min(len(flds), len(names)) {
		switch flds[i].typ {
		case scanner.Ident:
			args = append(args, fmt.Sprintf("%q:%q", names[i], flds[i].txt))
		default:
			args = append(args, fmt.Sprintf("%q:%s", names[i], flds[i].txt))
		}
	}
	return "{" + strings.Join(args, ",") + "}"
}

func toJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}
