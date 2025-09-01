package markdown

import (
	"embed"
	"os"
	"strings"
	"testing"
)

//go:embed docs
var docs embed.FS

var header_html = `
<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8">
    <style>
html, body {
    background: #252C33;
    color: white;
    font-family: 'Segoe UI', 'Roboto', 'Helvetica', sans-serif;
    font-size: 14px;
}

code {
    font-family: 'Ubuntu Mono', 'DejaVu Sans Mono', 'Menlo', monospace;
}

pre code {
    display: block;
    overflow-x: auto;
    padding: 1em;
    color: #fff;
    background: #1c1b1b;
}
    </style>

     <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.22/dist/katex.min.css" integrity="sha384-5TcZemv2l/9On385z///+d7MSYlvIEw9FuZTIdZ14vJLqWphw7e7ZPuOiCHJcFCP" crossorigin="anonymous">
  </head>
  <body>
`

var footer_html = `
  </body>
</html>
`

func TestMarkdown(t *testing.T) {
	files, err := docs.ReadDir("docs")
	if err != nil {
		t.Error(err)
	}
	for _, file := range files {
		md, err := docs.ReadFile("docs/" + file.Name())
		if err != nil {
			t.Error(err)
		}
		t.Log(file.Name())
		html, err := Render(string(md))
		if err != nil {
			t.Error(err)
		}
		dst := strings.TrimSuffix(file.Name(), ".md") + ".html"
		t.Log("write rendered output to", dst)
		err = os.WriteFile(dst, []byte(header_html+html+footer_html), 0644)
		if err != nil {
			t.Error(err)
		}
	}
}
