package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	openai "github.com/sashabaranov/go-openai"
)

func main() {
	config := openai.DefaultConfig("")
	config.BaseURL = "http://localhost:8080/v1"
	client := openai.NewClientWithConfig(config)

	req := openai.ChatCompletionRequest{
		Messages: []openai.ChatCompletionMessage{
			{Role: "user", Content: "What were the original aims of the go programming language?"},
		},
	}

	channel := ""
	printOutput := func(delta openai.ChatCompletionStreamChoiceDelta) {
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
	}

	finishReason, err := CreateChatCompletionStream(context.Background(), client, req, printOutput)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("\n\nfinish reason: %s\n", finishReason)
}

// Send streaming chat request to client and return updates on channel ch. Closes channel on completion.
func CreateChatCompletionStream(ctx context.Context, client *openai.Client, request openai.ChatCompletionRequest,
	callback func(openai.ChatCompletionStreamChoiceDelta)) (finishReason openai.FinishReason, err error) {

	stream, err := client.CreateChatCompletionStream(ctx, request)
	if err != nil {
		return finishReason, err
	}
	defer stream.Close()
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return finishReason, nil
		}
		if err != nil {
			return finishReason, err
		}
		if len(resp.Choices) != 0 {
			callback(resp.Choices[0].Delta)
			if resp.Choices[0].FinishReason != "" {
				finishReason = resp.Choices[0].FinishReason
			}
		}
	}
}
