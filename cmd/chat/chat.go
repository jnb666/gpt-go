package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jnb666/gpt-go/api"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/shared"
	log "github.com/sirupsen/logrus"
)

func main() {
	var nostream, openrouter bool
	var systemPrompt, reasoning string
	flag.StringVar(&reasoning, "reasoning", "medium", "set reasoning - low, medium or high")
	flag.StringVar(&systemPrompt, "system", "", "set custom system prompt")
	flag.BoolVar(&api.Debug, "debug", false, "enable debug logging")
	flag.BoolVar(&nostream, "nostream", false, "don't stream responses")
	flag.BoolVar(&openrouter, "openrouter", false, "use openrouter endpoint")
	flag.Parse()

	server := api.LlamaCpp
	if openrouter {
		server = api.OpenRouter
	}
	baseURL, modelName := api.DefaultModel(server)
	log.Infof("connecting to %s %s", baseURL, modelName)
	client := openai.NewClient(option.WithBaseURL(baseURL))

	req := openai.ChatCompletionNewParams{
		Model:           modelName,
		ReasoningEffort: shared.ReasoningEffort(reasoning),
	}
	if systemPrompt != "" {
		req.Messages = append(req.Messages, openai.SystemMessage(systemPrompt))
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
		if nostream {
			message, stats, err = api.ChatCompletion(ctx, client, req, server, printOutput, nil)
		} else {
			message, stats, err = api.ChatCompletionStream(ctx, client, req, server, printOutput, nil)
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
