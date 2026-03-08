package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/browser"
	"github.com/jnb666/gpt-go/api/tools/python"
	"github.com/jnb666/gpt-go/api/tools/weather"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	log "github.com/sirupsen/logrus"
)

var debug, nostream, useWeather, useBrowser, usePython bool

func main() {
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.BoolVar(&api.TraceRequests, "trace", false, "trace request and response messages")
	flag.BoolVar(&nostream, "nostream", false, "don't stream responses")
	flag.BoolVar(&useWeather, "weather", false, "enable weather tool")
	flag.BoolVar(&useBrowser, "browser", false, "enable browser tool")
	flag.BoolVar(&usePython, "python", false, "enable python tool")
	flag.Parse()
	if debug {
		log.SetLevel(log.DebugLevel)
	}

	server := api.VLLM
	baseURL, modelName := api.DefaultModel(server)
	log.Infof("connecting to %s %s", baseURL, modelName)
	client := openai.NewClient(option.WithBaseURL(baseURL))

	tools, browse, pyexec := initTools()
	defer browse.Close()
	defer pyexec.Stop()

	cfg := api.DefaultConfig(tools...)
	conv := api.NewConversation(cfg)

	input := bufio.NewReader(os.Stdin)
	ctx := context.Background()
	printOutput := printOutputFunc(browse)

	for {
		fmt.Print("> ")
		question, err := input.ReadString('\n')
		if err != nil {
			break
		}
		conv.Messages = append(conv.Messages, api.Message{Role: "user", Content: strings.TrimSpace(question)})
		req, err := api.NewRequest(modelName, conv, tools...)
		if err != nil {
			log.Fatal(err)
		}
		var msgs []api.Message
		if nostream {
			msgs, err = api.ChatCompletion(ctx, client, req, printOutput, logStats, tools...)
		} else {
			msgs, err = api.ChatCompletionStream(ctx, client, req, printOutput, logStats, tools...)
		}
		if err == nil {
			log.Debug(api.Pretty(msgs))
			conv.Messages = append(conv.Messages, msgs...)
		} else {
			log.Error(err)
			conv.Messages = conv.Messages[:len(conv.Messages)-1]
		}
	}
}

func logStats(stats api.Stats) {
	stats.Loginfo()
}

func initTools() (tools []api.ToolFunction, browse *browser.Browser, pyexec *python.Python) {
	if useWeather {
		tools = append(tools, weather.Tools(os.Getenv("OWM_API_KEY"))...)
	}
	if useBrowser {
		browse = browser.NewBrowser(os.Getenv("BRAVE_API_KEY"))
		tools = append(tools, browse.Tools()...)
	}
	if usePython {
		pyexec = python.New()
		tools = append(tools, pyexec)
	}
	var funcs []string
	for _, tool := range tools {
		funcs = append(funcs, tool.Definition().Name)
	}
	log.Info("tool functions: ", strings.Join(funcs, ", "))
	return
}

func printOutputFunc(browse *browser.Browser) api.CallbackFunc {
	return func(channel, content string, index int, end bool) {
		if index == 0 {
			fmt.Printf("\n== %s ==\n", channel)
		}
		if end && browse != nil {
			fmt.Println("== postprocessed ==")
			fmt.Print(browse.Postprocess(content))
		} else if index == 0 || !end {
			fmt.Print(content)
		}
	}
}
