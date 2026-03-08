package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jnb666/gpt-go/api"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	log "github.com/sirupsen/logrus"
)

func main() {
	var debug, nostream bool
	var systemPrompt, reasoning string
	flag.StringVar(&reasoning, "reasoning", "medium", "set reasoning - none, low, medium or high")
	flag.StringVar(&systemPrompt, "system", "", "set custom system prompt")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.BoolVar(&api.TraceRequests, "trace", false, "trace request and response messages")
	flag.BoolVar(&nostream, "nostream", false, "don't stream responses")
	flag.Parse()
	if debug {
		log.SetLevel(log.DebugLevel)
	}

	server := api.VLLM
	baseURL, modelName := api.DefaultModel(server)
	log.Infof("connecting to %s %s", baseURL, modelName)
	client := openai.NewClient(option.WithBaseURL(baseURL))

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
		req, err := api.NewRequest(modelName, conv)
		if err != nil {
			log.Fatal(err)
		}
		var msgs []api.Message
		if nostream {
			msgs, err = api.ChatCompletion(ctx, client, req, printOutput, logStats)
		} else {
			msgs, err = api.ChatCompletionStream(ctx, client, req, printOutput, logStats)
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
