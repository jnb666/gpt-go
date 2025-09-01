// Package api provides higher level functions to wrap the OpenAI chat completions API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/sashabaranov/go-openai"
)

// Interface implented by tools which can be called by the model.
type ToolFunction interface {
	Definition() openai.Tool
	Call(args json.RawMessage) string
}

// Send streaming chat request to client and calls callback func with updates. Returns accumulated response.
// If tools are defined and the response ends with a tool call then invoke the Call method adds the results
// to the request before resending.
func CreateChatCompletionStream(ctx context.Context, client *openai.Client, request openai.ChatCompletionRequest,
	callback func(openai.ChatCompletionStreamChoiceDelta) error, tools ...ToolFunction) (choice openai.ChatCompletionChoice, err error) {

	for {
		choice, err := createChatCompletionStream(ctx, client, request, callback)
		if err != nil || len(choice.Message.ToolCalls) == 0 {
			return choice, err
		}
		resp, err := callTool(choice.Message.ToolCalls[0].Function, tools)
		if err != nil {
			return choice, err
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
	callback func(openai.ChatCompletionStreamChoiceDelta) error) (choice openai.ChatCompletionChoice, err error) {

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
		if err = callback(delta); err != nil {
			return choice, err
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
