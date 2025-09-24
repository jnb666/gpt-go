// Package api provides higher level functions to wrap the OpenAI chat completions API.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/shared"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/pretty"
)

var (
	Debug   = false
	DebugTo = os.Stderr
)

func init() {
	pretty.DefaultOptions.Width = 120
}

// Interface implented by tools which can be called by the model.
type ToolFunction interface {
	// function name and argument schema
	Definition() shared.FunctionDefinitionParam
	// call with args in JSON format - if err is set it is treated as fatal, else the model can retry with new args
	Call(args string) (string, error)
}

// Tool parameters for given list of tools
func ChatCompletionToolParams(tools []ToolFunction) (params []openai.ChatCompletionToolUnionParam) {
	for _, tool := range tools {
		def := openai.ChatCompletionFunctionToolParam{Function: tool.Definition()}
		params = append(params, openai.ChatCompletionToolUnionParam{OfFunction: &def})
	}
	return params
}

// Default model settings
func DefaultModel(openrouter bool) (baseURL, modelName string) {
	if openrouter {
		baseURL = "https://openrouter.ai/api/v1"
		modelName = "@preset/gpt-oss-120"
	} else {
		baseURL = "http://deepthought:8080/v1"
	}
	return
}

// Get content and reasoning content from raw JSON message
func GetContent(raw string) (content, reasoning string) {
	var v struct {
		Content           string
		Refusal           string
		Reasoning         string
		Reasoning_Content string
	}
	json.Unmarshal([]byte(raw), &v)
	return v.Content + v.Refusal, v.Reasoning + v.Reasoning_Content
}

// Generated content where channel is either analysis (i.e. reasoning text), final (generated text) or tool (response from tool call) and
// index is the count of the message on the current stream, end is set on final message completion and full rather than delta content is sent
type CallbackFunc func(channel, content string, index int, end bool)

// Chat completion without streaming with optional function call support. The final assistant response content is returned along with usage and timing stats.
// callback is called after each stage of generation - i.e. reasoning text, tool response and final response.
func ChatCompletion(ctx context.Context, client openai.Client, req openai.ChatCompletionNewParams, callback CallbackFunc,
	tools ...ToolFunction) (message string, stats Stats, err error) {

	if Debug {
		Pprint(req.Messages)
	}
	stats = newStats()
	retry := 0
	maxRetries := 3
	for {
		// submit request
		start := time.Now()
		resp, err := client.Chat.Completions.New(ctx, req, newOptions(req))
		if err != nil {
			return "", stats, err
		}
		stats.update(resp.Model, resp.Usage, start)
		if Debug {
			Pprint(resp.RawJSON())
		}
		if len(resp.Choices) == 0 {
			return "", stats, getError(resp.RawJSON())
		}
		// parse response
		choice := resp.Choices[0]
		content, reasoning := GetContent(choice.Message.RawJSON())
		if reasoning != "" {
			callback("analysis", reasoning+"\n", 0, false)
		}
		if choice.FinishReason != "tool_calls" && content != "" {
			callback("final", content+"\n", 0, true)
			return content, stats, nil
		}
		if len(choice.Message.ToolCalls) == 0 && retry < maxRetries {
			// retry with current reasoning content if tool call is expected but missing
			if reasoning != "" {
				req.Messages = append(req.Messages, openai.AssistantMessage(reasoning))
			}
			retry++
			log.Infof("ChatCompletion: retry %d/%d", retry, maxRetries)
			continue
		}
		if len(choice.Message.ToolCalls) == 0 {
			return "", stats, fmt.Errorf("ChatCompletion: stop with empty response")
		}
		// have tool call - call function
		retry = 0
		call := choice.Message.ToolCalls[0]
		if call.Type != "function" {
			return "", stats, fmt.Errorf("ChatCompletion: %q tool call type not supported", call.Type)
		}
		fn := call.Function
		start = time.Now()
		toolResponse, err := callTool(fn.Name, fn.Arguments, tools)
		stats.toolCalled(fn.Name, start)
		if err != nil {
			return "", stats, err
		}
		callback("tool", toolResponse+"\n", 0, false)
		// add call and response to request and resend
		req.Messages = append(req.Messages,
			functionCallMessage(reasoning, call.ID, fn.Name, fn.Arguments),
			openai.ToolMessage(toolResponse, call.ID),
		)
	}
}

// As per ChatCompletion but will stream responses as they are generated generated
func ChatCompletionStream(ctx context.Context, client openai.Client, req openai.ChatCompletionNewParams, callback CallbackFunc,
	tools ...ToolFunction) (message string, stats Stats, err error) {

	if Debug {
		Pprint(req.Messages)
	}
	stats = newStats()
	retry := 0
	maxRetries := 3
	for {
		start := time.Now()
		acc, err := chatCompletionStream(ctx, client, req, callback)
		if err != nil {
			return "", stats, err
		}
		stats.update(acc.Model, acc.Usage, start)
		if len(acc.Choices) == 0 {
			return "", stats, getError(acc.RawJSON())
		}
		call, haveToolCall := acc.JustFinishedToolCall()
		if !haveToolCall && acc.Content != "" {
			callback("final", "\n", acc.index, false)
			callback("final", acc.Content+"\n", acc.index+1, true)
			return acc.Content, stats, nil
		}
		if !haveToolCall && retry < maxRetries {
			// retry with current reasoning content if tool call is expected but missing
			if acc.Reasoning != "" {
				req.Messages = append(req.Messages, openai.AssistantMessage(acc.Reasoning))
			}
			retry++
			log.Infof("ChatCompletion: retry %d/%d", retry, maxRetries)
			continue
		}
		if !haveToolCall {
			return "", stats, fmt.Errorf("ChatCompletion: stop with empty response")
		}
		// have tool call - call function
		retry = 0
		start = time.Now()
		toolResponse, err := callTool(call.Name, call.Arguments, tools)
		stats.toolCalled(call.Name, start)
		if err != nil {
			return "", stats, err
		}
		callback("tool", toolResponse+"\n", 0, false)
		req.Messages = append(req.Messages,
			functionCallMessage(acc.Reasoning, call.ID, call.Name, call.Arguments),
			openai.ToolMessage(toolResponse, call.ID),
		)
	}
}

type accumulator struct {
	openai.ChatCompletionAccumulator
	Content   string
	Reasoning string
	index     int
}

// send streaming request and accumulate response
func chatCompletionStream(ctx context.Context, client openai.Client, req openai.ChatCompletionNewParams, callback CallbackFunc) (
	acc accumulator, err error) {

	stream := client.Chat.Completions.NewStreaming(ctx, req, newOptions(req))
	channel := "analysis"
	for stream.Next() {
		chunk := stream.Current()
		if Debug {
			Pprint(chunk)
		}

		acc.AddChunk(chunk)
		if _, ok := acc.JustFinishedToolCall(); ok {
			if acc.index > 0 {
				callback(channel, "\n", acc.index, false)
			}
			break
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		content, reasoning := GetContent(chunk.Choices[0].Delta.RawJSON())
		acc.Content += content
		acc.Reasoning += reasoning
		if reasoning != "" {
			callback(channel, reasoning, acc.index, false)
			acc.index++
		}
		if content != "" {
			if channel == "analysis" {
				if acc.index > 0 {
					callback(channel, "\n", acc.index, false)
				}
				channel = "final"
				acc.index = 0
			}
			callback(channel, content, acc.index, false)
			acc.index++
		}
	}
	return acc, stream.Err()
}

// llama.cpp comparibility
func newOptions(req openai.ChatCompletionNewParams) option.RequestOption {
	kwargs := map[string]any{"reasoning_effort": req.ReasoningEffort}
	return option.WithJSONSet("chat_template_kwargs", kwargs)
}

// Pretty print JSON data if given string, or struct if given any other type
func Pprint(value any) {
	var data []byte
	if jsdata, ok := value.(string); ok {
		data = []byte(jsdata)
	} else {
		data, _ = json.Marshal(value)
	}
	data = pretty.Color(pretty.Pretty(data), nil)
	fmt.Fprintln(DebugTo, string(data))
}

// utilities
func getError(rawJSON string) error {
	type errorResponse struct {
		Code    int
		Message string
	}
	v := errorResponse{Code: 500, Message: "server error"}
	json.Unmarshal([]byte(rawJSON), &v)
	return fmt.Errorf("error %d: %s", v.Code, v.Message)
}

func callTool(name, args string, tools []ToolFunction) (string, error) {
	for _, tool := range tools {
		if tool.Definition().Name == name {
			return tool.Call(args)
		}
	}
	return "", fmt.Errorf("ChatCompletion: tool function %q not defined", name)
}

func functionCallMessage(reasoning, callID, functionName, arguments string) openai.ChatCompletionMessageParamUnion {
	msg := openai.AssistantMessage(reasoning) //"<|channel|>analysis<|message|>" + reasoning)
	var p openai.ChatCompletionMessageFunctionToolCallParam
	p.ID, p.Function.Name, p.Function.Arguments = callID, functionName, arguments
	msg.OfAssistant.ToolCalls = []openai.ChatCompletionMessageToolCallUnionParam{{OfFunction: &p}}
	return msg
}
