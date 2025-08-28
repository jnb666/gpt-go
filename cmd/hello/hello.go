package main

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

func main() {
	config := openai.DefaultConfig("")
	config.BaseURL = "http://localhost:8080/v1"
	client := openai.NewClientWithConfig(config)

	req := openai.ChatCompletionRequest{
		Messages: []openai.ChatCompletionMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	resp, err := client.CreateChatCompletion(context.Background(), req)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("== analysis ==\n%s\n== final ==\n%s\n",
		resp.Choices[0].Message.ReasoningContent, resp.Choices[0].Message.Content)
}
