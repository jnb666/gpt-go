// Package api provides higher level functions to wrap the OpenAI chat completions API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/sashabaranov/go-openai"
)

// Interface implented by tools which can be called by the model.
type ToolFunction interface {
	Definition() openai.Tool
	Call(args json.RawMessage) string
}

// Create a new openai client with default config.
// BaseURL is from OPENAI_BASE_URL env var if set else http://localhost:8080/v1"
func NewClient() *openai.Client {
	config := openai.DefaultConfig("")
	if url := os.Getenv("OPENAI_BASE_URL"); url != "" {
		config.BaseURL = url
	} else {
		config.BaseURL = "http://localhost:8080/v1"
	}
	return openai.NewClientWithConfig(config)
}

// Send streaming chat request to client and calls callback func with updates. Returns accumulated response.
// If tools are defined and the response ends with a tool call then invoke the Call method adds the results
// to the request before resending.
func CreateChatCompletionStream(ctx context.Context, client *openai.Client, request openai.ChatCompletionRequest,
	callback func(openai.ChatCompletionStreamChoiceDelta) error, tools ...ToolFunction) (choice openai.ChatCompletionChoice, usage *openai.Usage, err error) {

	reasoningTokens := 0
	for {
		choice, usage, err := createChatCompletionStream(ctx, client, request, callback)
		if usage != nil {
			usage.CompletionTokensDetails = &openai.CompletionTokensDetails{ReasoningTokens: reasoningTokens}
		}
		if err != nil || len(choice.Message.ToolCalls) == 0 {
			return choice, usage, err
		}
		if usage != nil {
			reasoningTokens += usage.CompletionTokens
		}
		resp, err := callTool(choice.Message.ToolCalls[0].Function, tools)
		if err != nil {
			return choice, usage, err
		}
		callback(openai.ChatCompletionStreamChoiceDelta{Role: openai.ChatMessageRoleTool, Content: resp})

		request.Messages = append(request.Messages,
			openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   "<|channel|>analysis<|message|>" + choice.Message.ReasoningContent,
				ToolCalls: choice.Message.ToolCalls,
			},
			openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleTool,
				Content: resp,
			},
		)
	}
}

func callTool(fn openai.FunctionCall, tools []ToolFunction) (string, error) {
	for _, tool := range tools {
		if tool.Definition().Function.Name == fn.Name {
			return tool.Call(json.RawMessage(fn.Arguments)), nil
		}
	}
	return "", fmt.Errorf("Error calling %q - tool function not defined", fn.Name)
}

// Send streaming chat request to client and calls callback func with updates. Returns accumulated response.
func createChatCompletionStream(ctx context.Context, client *openai.Client, request openai.ChatCompletionRequest,
	callback func(openai.ChatCompletionStreamChoiceDelta) error) (choice openai.ChatCompletionChoice, usage *openai.Usage, err error) {

	stream, err := client.CreateChatCompletionStream(ctx, request)
	if err != nil {
		return choice, usage, err
	}
	defer stream.Close()
	for {
		resp, err := stream.Recv()
		if resp.Usage != nil {
			usage = resp.Usage
		}
		if errors.Is(err, io.EOF) {
			return choice, usage, nil
		}
		if err != nil {
			return choice, usage, err
		}
		if len(resp.Choices) == 0 {
			continue
		}
		delta := resp.Choices[0].Delta
		if err = callback(delta); err != nil {
			return choice, usage, err
		}
		choice.Message.Content += delta.Content
		choice.Message.ReasoningContent += delta.ReasoningContent
		// assumes max of 1 tool call in message
		if len(delta.ToolCalls) > 0 {
			if len(choice.Message.ToolCalls) == 0 {
				choice.Message.ToolCalls = delta.ToolCalls
			} else {
				choice.Message.ToolCalls[0].Function.Arguments += delta.ToolCalls[0].Function.Arguments
			}
		}
		if resp.Choices[0].FinishReason != "" {
			choice.FinishReason = resp.Choices[0].FinishReason
		}
	}
}
