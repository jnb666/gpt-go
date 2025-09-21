package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/browser"
	"github.com/sashabaranov/go-openai"
	log "github.com/sirupsen/logrus"
)

func main() {
	client := api.NewClient()
	browse := browser.NewBrowser(os.Getenv("BRAVE_API_KEY"))
	defer browse.Close()

	tools := browse.Tools()
	cfg := api.DefaultConfig(tools...)
	cfg.ToolDescription = browse.Description()

	conv := api.NewConversation(cfg)
	conv.Messages = append(conv.Messages, api.Message{Type: "user", Content: "who is Prime Minister of the UK?"})

	req := api.NewRequest(conv, tools...)

	resp, _, err := api.CreateChatCompletionStream(context.Background(), client, req, printOutput(), tools...)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println()
	fmt.Printf("\n## postprocesssed\n%s\n", browse.Postprocess(resp.Message.Content))
}

// Print output from chat completion stream to stdout
func printOutput() func(openai.ChatCompletionStreamChoiceDelta) error {
	channel := ""
	return func(delta openai.ChatCompletionStreamChoiceDelta) error {
		if delta.Role == "tool" {
			fmt.Printf("\n## tool response\n%s\n", delta.Content)
			return nil
		}
		if delta.ReasoningContent != "" {
			if channel == "" {
				fmt.Println("## analysis")
				channel = "analysis"
			}
			fmt.Print(delta.ReasoningContent)
		}
		if delta.Content != "" {
			if channel == "analysis" {
				fmt.Println("\n\n## final")
				channel = "final"
			}
			fmt.Print(delta.Content)
		}
		if len(delta.ToolCalls) > 0 && delta.ToolCalls[0].Function.Name != "" {
			fmt.Println("\n\n## tool call")
			return nil
		}
		return nil
	}
}

func pretty(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(data)
}
