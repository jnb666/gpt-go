package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/jnb666/gpt-go/api"
	openai "github.com/sashabaranov/go-openai"
)

func main() {
	client := api.NewClient()
	var req openai.ChatCompletionRequest

	input := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		question, err := input.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}
		req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: "user", Content: question})

		resp, err := createChatCompletionStream(context.Background(), client, req, printOutput())
		if err != nil {
			log.Fatal(err)

		}
		fmt.Println()
		req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: "assistant", Content: resp.Message.Content})
	}
}

// Print output from chat completion stream to stdout
func printOutput() func(openai.ChatCompletionStreamChoiceDelta) {
	channel := ""
	return func(delta openai.ChatCompletionStreamChoiceDelta) {
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
}

// Send streaming chat request to client and calls callback func with updates. Returns accumulated response.
func createChatCompletionStream(ctx context.Context, client *openai.Client, request openai.ChatCompletionRequest,
	callback func(openai.ChatCompletionStreamChoiceDelta)) (choice openai.ChatCompletionChoice, err error) {

	stream, err := client.CreateChatCompletionStream(ctx, request)
	if err != nil {
		return choice, err
	}
	defer stream.Close()
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return choice, nil
		}
		if err != nil {
			return choice, err
		}
		if len(resp.Choices) == 0 {
			continue
		}
		delta := resp.Choices[0].Delta
		callback(delta)
		choice.Message.Content += delta.Content
		choice.Message.ReasoningContent += delta.ReasoningContent
		if resp.Choices[0].FinishReason != "" {
			choice.FinishReason = resp.Choices[0].FinishReason
		}
	}
}
