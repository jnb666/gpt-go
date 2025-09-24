package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jnb666/gpt-go/api"
	"github.com/openai/openai-go/v2"
)

func main() {
	fmt.Println("connecting to", os.Getenv("OPENAI_BASE_URL"))
	client := openai.NewClient()

	question := "Hello"
	req := openai.ChatCompletionNewParams{
		Model: "@preset/gpt-oss-120",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(question),
		},
	}
	fmt.Printf("== user ==\n%s\n", question)

	resp, err := client.Chat.Completions.New(context.Background(), req)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	content, reasoning := api.GetContent(resp.Choices[0].Message.RawJSON())
	fmt.Printf("== analysis ==\n%s\n== final ==\n%s\n", reasoning, content)
}
