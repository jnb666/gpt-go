package main

import (
	"context"
	"fmt"

	"github.com/jnb666/gpt-go/api"
	openai "github.com/sashabaranov/go-openai"
)

func main() {
	client := api.NewClient()
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
