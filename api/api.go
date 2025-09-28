// Package api provides higher level functions to wrap the OpenAI chat completions API.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
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

const (
	LlamaCpp   Server = 0
	OpenRouter Server = 1
)

type Server int

// Interface implented by tools which can be called by the model.
type ToolFunction interface {
	// function name and argument schema
	Definition() shared.FunctionDefinitionParam
	// call with args in JSON format - if err is set it is treated as fatal, else the model can retry with new args
	Call(args string) (req, resp string, err error)
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
func DefaultModel(server Server) (baseURL, modelName string) {
	switch server {
	case LlamaCpp:
		baseURL = "http://deepthought:8080/v1"
	case OpenRouter:
		baseURL = "https://openrouter.ai/api/v1"
		modelName = "@preset/gpt-oss-120"
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
func ChatCompletion(ctx context.Context, client openai.Client, request openai.ChatCompletionNewParams, server Server, callback CallbackFunc, statsCallback func(Stats),
	tools ...ToolFunction) (message string, stats Stats, err error) {

	stats = newStats()
	req := request
	req.Messages = slices.Clone(request.Messages)
	var content, reasoning string
	for {
		// submit request
		opts := requestOptions(req, server, reasoning)
		start := time.Now()
		resp, err := client.Chat.Completions.New(ctx, req, opts...)
		if err != nil {
			return "", stats, err
		}
		stats.update(resp.Model, resp.Usage, start)
		if statsCallback != nil {
			statsCallback(stats)
		}
		if len(resp.Choices) == 0 {
			return "", stats, getError(resp.RawJSON())
		}
		// parse response
		choice := resp.Choices[0]
		content, reasoning = GetContent(choice.Message.RawJSON())
		if reasoning != "" {
			callback("analysis", reasoning+"\n", 0, false)
		}
		if choice.FinishReason != "tool_calls" && content != "" {
			callback("final", content+"\n", 0, true)
			return content, stats, nil
		}
		if len(choice.Message.ToolCalls) == 0 {
			return "", stats, fmt.Errorf("ChatCompletion: stop with empty response")
		}
		// have tool call - call function
		call := choice.Message.ToolCalls[0]
		if call.Type != "function" {
			return "", stats, fmt.Errorf("ChatCompletion: %q tool call type not supported", call.Type)
		}
		fn := call.Function
		start = time.Now()
		toolReq, toolResp, err := callTool(fn.Name, fn.Arguments, tools)
		stats.toolCalled(fn.Name, start)
		if err != nil {
			toolResp = fmt.Sprintf("error calling %s: %v", fn.Name, err)
			log.Error(toolResp)
		}
		callback("tool", toolReq+"\n"+toolResp+"\n", 0, false)
		// add call and response to request and resend
		req.Messages = append(req.Messages, choice.Message.ToParam(), openai.ToolMessage(toolResp, call.ID))
	}
}

// As per ChatCompletion but will stream responses as they are generated generated
func ChatCompletionStream(ctx context.Context, client openai.Client, request openai.ChatCompletionNewParams, server Server, callback CallbackFunc, statsCallback func(Stats),
	tools ...ToolFunction) (message string, stats Stats, err error) {

	stats = newStats()
	req := request
	req.Messages = slices.Clone(request.Messages)
	req.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
	var acc Accumulator
	for {
		opts := requestOptions(req, server, acc.Reasoning)
		start := time.Now()
		acc, err = chatCompletionStream(ctx, client, req, opts, callback)
		if err != nil {
			return "", stats, err
		}
		stats.update(acc.Model, acc.Usage, start)
		if len(acc.Choices) == 0 {
			return "", stats, getError(acc.RawJSON())
		}
		if statsCallback != nil {
			statsCallback(stats)
		}
		call, haveToolCall := acc.JustFinishedToolCall()
		if !haveToolCall && acc.Content != "" {
			callback("final", "\n", acc.index, false)
			callback("final", acc.Content+"\n", acc.index+1, true)
			return acc.Content, stats, nil
		}
		if !haveToolCall {
			return "", stats, fmt.Errorf("ChatCompletion: stop with empty response")
		}
		// have tool call - call function
		start = time.Now()
		toolReq, toolResp, err := callTool(call.Name, call.Arguments, tools)
		stats.toolCalled(call.Name, start)
		if err != nil {
			toolResp = fmt.Sprintf("error calling %s: %v", call.Name, err)
			log.Error(toolResp)
		}
		callback("tool", toolReq+"\n"+toolResp+"\n", 0, false)
		// add call and response to request and resend
		req.Messages = append(req.Messages, acc.Choices[0].Message.ToParam(), openai.ToolMessage(toolResp, call.ID))
	}
}

type Accumulator struct {
	openai.ChatCompletionAccumulator
	Content   string
	Reasoning string
	index     int
}

// send streaming request and accumulate response
func chatCompletionStream(ctx context.Context, client openai.Client, req openai.ChatCompletionNewParams, opts []option.RequestOption, callback CallbackFunc) (
	acc Accumulator, err error) {

	stream := client.Chat.Completions.NewStreaming(ctx, req, opts...)
	channel := "analysis"
	for stream.Next() {
		chunk := stream.Current()
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

// extra JSON fields to set in request
func requestOptions(req openai.ChatCompletionNewParams, server Server, reasoning string) (opts []option.RequestOption) {
	if reasoning != "" && len(req.Messages) > 2 {
		ix := len(req.Messages) - 2
		lastCall := req.Messages[ix]
		if lastCall.OfAssistant != nil && param.IsOmitted(lastCall.OfAssistant.Content) {
			switch server {
			case LlamaCpp:
				opts = append(opts, option.WithJSONSet("messages."+strconv.Itoa(ix)+".thinking", reasoning))
			case OpenRouter:
				opts = append(opts, option.WithJSONSet("messages."+strconv.Itoa(ix)+".reasoning", reasoning))
			}
		}
	}
	if server == LlamaCpp && req.ReasoningEffort != "" {
		opts = append(opts, option.WithJSONSet("chat_template_kwargs", map[string]any{"reasoning_effort": req.ReasoningEffort}))
	}
	if Debug {
		opts = append(opts, option.WithMiddleware(debugLogger))
	}
	return opts
}

// utilities
func debugLogger(req *http.Request, nxt option.MiddlewareNext) (*http.Response, error) {
	req.Body = pprint("request", req.Body)
	resp, err := nxt(req)
	if err != nil {
		return resp, err
	}
	resp.Body = pprint("response", resp.Body)
	return resp, nil
}

func pprint(title string, body io.ReadCloser) io.ReadCloser {
	data, err := io.ReadAll(body)
	if err != nil {
		log.Error(err)
	}
	formatted := pretty.Color(pretty.Pretty(data), nil)
	fmt.Fprintf(DebugTo, "== %s ==\n%s\n", title, formatted)
	return io.NopCloser(bytes.NewBuffer(data))
}

func getError(rawJSON string) error {
	type errorResponse struct {
		Code    int
		Message string
	}
	v := errorResponse{Code: 500, Message: "server error"}
	json.Unmarshal([]byte(rawJSON), &v)
	return fmt.Errorf("error %d: %s", v.Code, v.Message)
}

func callTool(name, args string, tools []ToolFunction) (req, res string, err error) {
	for _, tool := range tools {
		if tool.Definition().Name == name {
			return tool.Call(args)
		}
	}
	return "", "", fmt.Errorf("ChatCompletion: tool function %q not defined", name)
}
