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
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	log "github.com/sirupsen/logrus"
)

var debug, nostream, openrouter, useWeather, useBrowser, usePython bool

func main() {
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.BoolVar(&api.Debug, "trace", false, "trace request and response messages")
	flag.BoolVar(&nostream, "nostream", false, "don't stream responses")
	flag.BoolVar(&openrouter, "openrouter", false, "use openrouter endpoint")
	flag.BoolVar(&useWeather, "weather", false, "enable weather tool")
	flag.BoolVar(&useBrowser, "browser", false, "enable browser tool")
	flag.BoolVar(&usePython, "python", false, "enable python tool")
	flag.Parse()

	if debug {
		log.SetLevel(log.DebugLevel)
	}
	server := api.LlamaCpp
	if openrouter {
		server = api.OpenRouter
	}
	baseURL, modelName := api.DefaultModel(server)
	log.Infof("connecting to %s %s", baseURL, modelName)
	client := openai.NewClient(option.WithBaseURL(baseURL))

	tools, browse, pyexec := initTools()
	defer browse.Close()
	defer pyexec.Stop()

	req := openai.ChatCompletionNewParams{
		Model: modelName,
		Tools: api.ChatCompletionToolParams(tools),
	}

	input := bufio.NewReader(os.Stdin)
	ctx := context.Background()
	printOutput := printOutputFunc(browse)

	for {
		fmt.Print("> ")
		question, err := input.ReadString('\n')
		if err != nil {
			break
		}
		req.Messages = append(req.Messages, openai.UserMessage(strings.TrimSpace(question)))
		browse.Reset()
		pyexec.Stop()
		var message string
		var stats api.Stats
		if nostream {
			message, stats, err = api.ChatCompletion(ctx, client, req, server, printOutput, nil, tools...)
		} else {
			message, stats, err = api.ChatCompletionStream(ctx, client, req, server, printOutput, nil, tools...)
		}
		fmt.Println()
		stats.Loginfo()
		if err != nil {
			log.Error(err)
		}
		req.Messages = append(req.Messages, openai.AssistantMessage(message))
	}
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
			fmt.Printf("== %s ==\n", channel)
		}
		if end && browse != nil {
			fmt.Println("== postprocessed ==")
			fmt.Print(browse.Postprocess(content))
		} else if index == 0 || !end {
			fmt.Print(content)
		}
	}
}
