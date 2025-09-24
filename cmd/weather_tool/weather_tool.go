package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/weather"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	log "github.com/sirupsen/logrus"
)

func main() {
	var stream, openrouter bool
	flag.BoolVar(&api.Debug, "debug", false, "enable debug logging")
	flag.BoolVar(&stream, "stream", false, "stream responses")
	flag.BoolVar(&openrouter, "openrouter", false, "use openrouter endpoint")
	flag.Parse()

	baseURL, modelName := api.DefaultModel(openrouter)
	log.Infof("connecting to %s %s", baseURL, modelName)
	client := openai.NewClient(option.WithBaseURL(baseURL))

	tools := weather.Tools(os.Getenv("OWM_API_KEY"))

	req := openai.ChatCompletionNewParams{
		Model: modelName,
		Tools: api.ChatCompletionToolParams(tools),
	}
	if api.Debug {
		api.Pprint(req)
	}

	input := bufio.NewReader(os.Stdin)
	ctx := context.Background()

	for {
		fmt.Print("> ")
		question, err := input.ReadString('\n')
		if err != nil {
			break
		}
		req.Messages = append(req.Messages, openai.UserMessage(strings.TrimSpace(question)))
		var message string
		var stats api.Stats
		if stream {
			message, stats, err = api.ChatCompletionStream(ctx, client, req, printOutput, tools...)
		} else {
			message, stats, err = api.ChatCompletion(ctx, client, req, printOutput, tools...)
		}
		fmt.Println()
		stats.Loginfo()
		if err != nil {
			log.Error(err)
		}
		req.Messages = append(req.Messages, openai.AssistantMessage(message))
	}
}

func printOutput(channel, content string, index int, end bool) {
	if index == 0 {
		fmt.Printf("== %s ==\n", channel)
	}
	if index == 0 || !end {
		fmt.Print(content)
	}
}
