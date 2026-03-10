package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jnb666/gpt-go/api"
	log "github.com/sirupsen/logrus"
)

func main() {
	var debug, nostream bool
	var systemPrompt, reasoning, modelName string
	var endpoint int
	flag.StringVar(&reasoning, "reasoning", "medium", "set reasoning - none, low, medium or high")
	flag.StringVar(&systemPrompt, "system", "", "set custom system prompt")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.BoolVar(&api.TraceRequests, "trace", false, "trace request and response messages")
	flag.BoolVar(&nostream, "nostream", false, "don't stream responses")
	flag.IntVar(&endpoint, "endpoint", 0, "openai server endpoint to use: 0=LlamaCPP 1=vLLM 2=OpenRouter 3=Cerebras")
	flag.StringVar(&modelName, "model", "", "model name - optional for local server")
	flag.Parse()
	if debug {
		log.SetLevel(log.DebugLevel)
	}
	client, err := api.NewClient(api.Server(endpoint), modelName)
	if err != nil {
		log.Fatal(err)
	}
	cfg := api.DefaultConfig()
	cfg.ReasoningEffort = reasoning
	if systemPrompt != "" {
		cfg.SystemPrompt = systemPrompt
	}
	conv := api.NewConversation(cfg)

	input := bufio.NewReader(os.Stdin)
	ctx := context.Background()

	for {
		fmt.Print("> ")
		question, err := input.ReadString('\n')
		if err != nil {
			break
		}
		conv.Messages = append(conv.Messages, api.Message{Role: "user", Content: strings.TrimSpace(question)})
		var msgs []api.Message
		if nostream {
			msgs, err = client.ChatCompletion(ctx, conv, printOutput, logStats)
		} else {
			msgs, err = client.ChatCompletionStream(ctx, conv, printOutput, logStats)
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

func printOutput(channel, content string, index int, end bool) {
	if index == 0 {
		fmt.Printf("\n== %s ==\n", channel)
	}
	if index == 0 || !end {
		fmt.Print(content)
	}
}
