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
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/pretty"
)

//go:generate stringer -type Server

var (
	// Optional logging of raw JSON requests and responses
	TraceRequests = false
	TraceStream   = false
	TraceTo       = os.Stderr
)

func init() {
	pretty.DefaultOptions.Width = 120
}

const (
	LlamaCPP   Server = 0
	VLLM       Server = 1
	OpenRouter Server = 2
	Cerebras   Server = 3
)

// Endpoint to connect to
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

// Get current server from LLM_SERVER env if set
func GetServer() Server {
	switch strings.ToLower(os.Getenv("LLM_SERVER")) {
	case "llamacpp":
		return LlamaCPP
	case "vllm":
		return VLLM
	case "openrouter":
		return OpenRouter
	case "cerebras":
		return Cerebras
	default:
		return -1
	}
}

// Client and associated info
type Client struct {
	openai.Client
	Server         Server
	BaseURL        string
	ModelName      string
	ReasoningField string
	ContextLength  int
}

// Create new client with default settings if no options are given.
// If set then will use OPENAI_BASE_URL and OPENAI_API_KEY environment variables.
// The model name is optional for LlamaCPP and LLLM - they will use the currently loaded model.
func NewClient(server Server, modelName string, opts ...option.RequestOption) (c Client, err error) {
	c = Client{Server: server, ReasoningField: "reasoning"}
	switch server {
	case LlamaCPP:
		c.BaseURL = "http://localhost:8080/v1"
		c.ReasoningField = "reasoning_content"
	case VLLM:
		c.BaseURL = "http://localhost:8080/v1"
	case OpenRouter:
		c.BaseURL = "https://openrouter.ai/api/v1"
		c.ModelName = "@preset/gpt-oss-120"
	case Cerebras:
		c.BaseURL = "https://api.cerebras.ai/v1"
		c.ModelName = "gpt-oss-120b"
	}
	if modelName != "" {
		c.ModelName = modelName
	}
	if url := os.Getenv("OPENAI_BASE_URL"); url != "" {
		c.BaseURL = url
	}
	log.Infof("connecting to %s at %s %s", server, c.BaseURL, c.ModelName)
	opts = append([]option.RequestOption{option.WithBaseURL(c.BaseURL)}, opts...)
	c.Client = openai.NewClient(opts...)
	if server == LlamaCPP || server == VLLM {
		c.ContextLength, err = MaxModelLength(server, c.BaseURL)
	}
	return c, err
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

// Chat completion without streaming with optional function call support. The list of new generated messages are returned.
// callback and statsCallback are called after each stage of generation - i.e. reasoning text, tool response and final response.
func (c *Client) ChatCompletion(ctx context.Context, request Conversation, callback CallbackFunc, statsCallback func(Stats), tools ...ToolFunction) ([]Message, error) {
	stats := newStats()
	conv := request
	conv.Messages = slices.Clone(request.Messages)
	var content, reasoning string
	retries := 0
	maxRetries := 3
	for {
		req := c.NewRequest(c.ModelName, conv, tools...)
		// optionally exclude previous messages if reached context threshold
		if conv.Config.CompactThreshold > 0 && conv.Config.CompactThreshold < 1 {
			limit := int(float64(c.ContextLength) * conv.Config.CompactThreshold)
			c.CompactMessages(request, limit)
		}
		// submit request
		opts := requestOptions(&req)
		start := time.Now()
		resp, err := c.Chat.Completions.New(ctx, req, opts...)
		if err != nil {
			return nil, err
		}
		if len(resp.Choices) == 0 {
			return nil, getError(resp.RawJSON())
		}
		stats.update(resp.Model, resp.Usage, start)
		// parse response
		message := resp.Choices[0].Message
		content, reasoning = GetContent(message.RawJSON())
		if isSet(reasoning) {
			callback("analysis", reasoning, 0, true)
		}
		if len(message.ToolCalls) == 0 {
			if isSet(content) {
				break
			} else if retries >= maxRetries {
				content = fmt.Sprintf("Error: giving up on request after %d retries", maxRetries)
				break
			} else {
				retries++
				log.Warnf("no message content or tool call - retry %d/%d reasoning=%q", retries, maxRetries, reasoning)
				continue
			}
		}
		retries = 0
		// have tool calls - call function and resend
		conv.Messages = append(conv.Messages, Message{Role: "assistant", Reasoning: reasoning, ToolCall: marshal(message.ToolCalls)})
		for _, call := range message.ToolCalls {
			toolID, toolResp := callTool(call, tools, &stats, callback)
			conv.Messages = append(conv.Messages, Message{Role: "tool", Content: toolResp, ToolCallID: toolID})
		}
		if statsCallback != nil {
			statsCallback(stats)
		}
	}
	callback("final", content+"\n", 0, true)
	if statsCallback != nil {
		statsCallback(stats)
	}
	msgs := append(conv.Messages[len(request.Messages):], Message{Role: "assistant", Content: content, Reasoning: reasoning})
	return msgs, nil
}

// As per ChatCompletion but will stream responses as they are generated.
func (c *Client) ChatCompletionStream(ctx context.Context, request Conversation, callback CallbackFunc, statsCallback func(Stats), tools ...ToolFunction) ([]Message, error) {
	stats := newStats()
	conv := request
	conv.Messages = slices.Clone(conv.Messages)
	var acc Accumulator
	retries := 0
	maxRetries := 3
	for {
		req := c.NewRequest(c.ModelName, conv, tools...)
		req.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
		// optionally exclude previous messages if reached context threshold
		if conv.Config.CompactThreshold > 0 && conv.Config.CompactThreshold < 1 {
			limit := int(float64(c.ContextLength) * conv.Config.CompactThreshold)
			c.CompactMessages(request, limit)
		}
		// submit streaming request
		opts := requestOptions(&req)
		start := time.Now()
		var err error
		acc, err = chatCompletionStream(ctx, c.Client, req, opts, callback)
		if err != nil {
			return nil, err
		}
		stats.update(acc.Model, acc.Usage, start)
		if len(acc.Choices) == 0 {
			return nil, getError(acc.RawJSON())
		}
		// parse response
		message := acc.Choices[0].Message
		if len(message.ToolCalls) == 0 {
			if isSet(acc.Content) {
				break
			} else if retries >= maxRetries {
				acc.Content = fmt.Sprintf("Error: giving up on request after %d retries", maxRetries)
				break
			} else {
				retries++
				log.Warnf("no message content or tool call - retry %d/%d reasoning=%q", retries, maxRetries, acc.Reasoning)
				continue
			}
		}
		retries = 0
		// have tool call - call function and resend
		conv.Messages = append(conv.Messages, Message{Role: "assistant", Reasoning: acc.Reasoning, ToolCall: marshal(message.ToolCalls)})
		for _, call := range message.ToolCalls {
			toolID, toolResp := callTool(call, tools, &stats, callback)
			conv.Messages = append(conv.Messages, Message{Role: "tool", Content: toolResp, ToolCallID: toolID})
		}
		if statsCallback != nil {
			statsCallback(stats)
		}
	}
	callback("final", "\n", acc.index, false)
	callback("final", acc.Content+"\n", acc.index+1, true)
	if statsCallback != nil {
		statsCallback(stats)
	}
	msgs := append(conv.Messages[len(request.Messages):], Message{Role: "assistant", Content: acc.Content, Reasoning: acc.Reasoning})
	return msgs, nil
}

// call tools, update stats and call callback with request and response text
func callTool(call openai.ChatCompletionMessageToolCallUnion, tools []ToolFunction, stats *Stats, callback CallbackFunc) (id, res string) {
	fn := call.Function
	for _, tool := range tools {
		if tool.Definition().Name == fn.Name {
			start := time.Now()
			req, resp, err := tool.Call(fn.Arguments)
			stats.toolCalled(fn.Name, start)
			if err != nil {
				resp = fmt.Sprintf("Error calling %s function: %v", fn.Name, err)
				log.Error(resp)
			}
			callback("tool", req+"\n"+resp+"\n", 0, false)
			return call.ID, resp
		}
	}
	return call.ID, fmt.Sprintf("Error: function %q is not defined", fn.Name)
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
		if TraceStream {
			pprint("chunk", chunk.RawJSON())
		}
		acc.AddChunk(chunk)
		if _, ok := acc.JustFinishedToolCall(); ok {
			if acc.index > 0 {
				callback(channel, "\n", acc.index, false)
			}
		}
		if len(chunk.Choices) > 0 {
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
	}
	return acc, stream.Err()
}

// extra JSON fields to set in request
func requestOptions(req *openai.ChatCompletionNewParams) (opts []option.RequestOption) {
	kwargs := map[string]any{}
	if req.ReasoningEffort == "none" {
		kwargs["enable_thinking"] = false
		req.ReasoningEffort = ""
	} else if req.ReasoningEffort != "" {
		kwargs["reasoning_effort"] = req.ReasoningEffort
	}
	if len(kwargs) > 0 {
		opts = append(opts, option.WithJSONSet("chat_template_kwargs", kwargs))
	}
	if TraceRequests {
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

func pprint(title string, value any) io.ReadCloser {
	var data []byte
	var err error
	switch v := value.(type) {
	case string:
		data = []byte(v)
	case io.ReadCloser:
		data, err = io.ReadAll(v)
		if err != nil {
			log.Error(err)
		}
	default:
		panic(fmt.Errorf("invalid type: %T", value))
	}
	fmt.Fprintf(TraceTo, "== %s ==\n%s\n", title, pretty.Pretty(data))
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

func isSet(s string) bool {
	return trim(s) != ""
}

func trim(s string) string {
	return strings.TrimSpace(s)
}
