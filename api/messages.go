package api

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
	log "github.com/sirupsen/logrus"
)

var DefaultSystemMessage = "You are a helpful assistant. You should answer concisely unless more detail is requested. The current date is {{today}}."

// Chat API request from frontend to webserver
type Request struct {
	Action  string  `json:"action"`           // add | list | load | delete | config
	ID      string  `json:"id,omitzero"`      // if action=load,delete uuid format
	Message Message `json:"message,omitzero"` // if action=add
	Config  *Config `json:"config,omitzero"`  // if action=config
}

// Chat API response from webserver back to frontend
type Response struct {
	Action       string       `json:"action"`                // add | list | load | config | stats
	Message      Message      `json:"message,omitzero"`      // if action=add
	Conversation Conversation `json:"conversation,omitzero"` // if action=load
	List         []Item       `json:"list,omitzero"`         // if action=list
	Config       Config       `json:"config,omitzero"`       // if action=config
	Stats        Stats        `json:"stats,omitzero"`        // if action=stats
}

type Conversation struct {
	ID       string    `json:"id"`
	Config   Config    `json:"config"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role            string          `json:"role"`            // user | assistant | tool
	Update          bool            `json:"update,omitzero"` // true if update to existing message
	End             bool            `json:"end,omitzero"`    // true if update and message is now complete
	Content         string          `json:"content"`
	Reasoning       string          `json:"reasoning,omitzero"`
	ToolCall        json.RawMessage `json:"tool_call,omitzero"`
	ToolCallID      string          `json:"tool_call_id,omitzero"`
	ContentTokens   int             `json:"content_tokens,omitzero"`
	ReasoningTokens int             `json:"reasoning_tokens,omitzero"`
	Excluded        bool            `json:"excluded,omitzero"` // message is ignored by NewRequest if this is set
}

type Item struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

type Config struct {
	SystemPrompt      string       `json:"system_prompt"`
	ReasoningEffort   string       `json:"reasoning_effort"` // low | medium | high | none
	Tools             []ToolConfig `json:"tools,omitzero"`
	Temperature       float64      `json:"temperature,omitzero"`
	TopP              float64      `json:"top_p,omitzero"`
	TopK              int          `json:"top_k,omitzero"`
	PresencePenalty   float64      `json:"presence_penalty,omitzero"`
	RepetitionPenalty float64      `json:"repetition_penalty,omitzero"`
}

type ToolConfig struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type Stats struct {
	Model            string         `json:"model"`             // model name
	ApiCalls         int            `json:"api_calls"`         // total number of API calls
	ApiTime          int            `json:"api_time"`          // total elapsed time in API calls in msec
	CompletionTokens int            `json:"completion_tokens"` // no. of completion tokens generated
	PromptTokens     int            `json:"prompt_tokens"`     // max prompt length
	ToolCalls        int            `json:"tool_calls"`        // total number of tool calls
	Functions        map[string]int `json:"functions"`         // numer of tool calls by function name
	ToolTime         int            `json:"tool_time"`         // total elapsed time in tool calls in msec
}

func newStats() Stats {
	return Stats{Functions: map[string]int{}}
}

func (s *Stats) APICallInfo() string {
	return fmt.Sprintf("%d API calls in %s  %d prompt tokens  %d completion tokens at %.1f tok/sec",
		s.ApiCalls, msec(s.ApiTime), s.PromptTokens, s.CompletionTokens, s.CompletionTokensPerSec())
}

func (s *Stats) ToolCallInfo() string {
	funcs := fmt.Sprint(s.Functions)
	return fmt.Sprintf("%d tool calls in %s - %s", s.ToolCalls, msec(s.ToolTime), funcs[4:len(funcs)-1])
}

func (s *Stats) Loginfo() {
	log.Info(s.APICallInfo())
	if s.ToolCalls > 0 {
		log.Info(s.ToolCallInfo())
	}
}

func (s *Stats) CompletionTokensPerSec() float64 {
	if s.ApiTime > 0 {
		return 1000 * float64(s.CompletionTokens) / float64(s.ApiTime)
	}
	return 0
}

func (s *Stats) update(model string, u openai.CompletionUsage, start time.Time) {
	s.Model = model
	s.ApiCalls++
	s.ApiTime += int(time.Since(start).Milliseconds())
	s.CompletionTokens += int(u.CompletionTokens)
	s.PromptTokens = int(u.PromptTokens)
}

func (s *Stats) toolCalled(name string, start time.Time) {
	s.ToolCalls++
	s.Functions[name]++
	s.ToolTime += int(time.Since(start).Milliseconds())
}

// Get default configuration with given tools enabled
func DefaultConfig(tools ...ToolFunction) Config {
	cfg := Config{
		SystemPrompt:      DefaultSystemMessage,
		ReasoningEffort:   "medium",
		Temperature:       1.0,
		TopP:              0.95,
		TopK:              20,
		PresencePenalty:   1.5,
		RepetitionPenalty: 1.0,
	}
	for _, tool := range tools {
		cfg.Tools = append(cfg.Tools, ToolConfig{Name: tool.Definition().Name, Enabled: true})
	}
	return cfg
}

// Create a new conversation and assign unique ID
func NewConversation(cfg Config) Conversation {
	return Conversation{
		ID:     uuid.Must(uuid.NewV7()).String(),
		Config: cfg,
	}
}

// Most recent non-excluded user message number or -1 if not found
func (c Conversation) LastUserMessageNumber() int {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if m := c.Messages[i]; !m.Excluded && m.Role == "user" {
			return i
		}
	}
	return -1
}

// Convert from openai chat completion message to our message format
func ToMessage(m openai.ChatCompletionMessageParamUnion) Message {
	var msg Message
	if role := m.GetRole(); role != nil {
		msg.Role = *role
	}
	if content, ok := m.GetContent().AsAny().(*string); ok {
		msg.Content = *content
	}
	if extra := m.ExtraFields(); extra != nil {
		if text, ok := extra[ReasoningField].(string); ok {
			msg.Reasoning = text
		}
	}
	if m.OfAssistant != nil && len(m.OfAssistant.ToolCalls) > 0 {
		data, err := json.Marshal(m.OfAssistant.ToolCalls)
		if err != nil {
			panic(err)
		}
		msg.ToolCall = data
	}
	if m.OfTool != nil {
		msg.Role = "tool"
		msg.ToolCallID = m.OfTool.ToolCallID
	}
	return msg
}

// Convert our message format to openai chat completion struct
func FromMessage(m Message, includeReasoning bool) (msg openai.ChatCompletionMessageParamUnion, err error) {
	switch m.Role {
	case "user":
		return openai.UserMessage(m.Content), nil
	case "assistant":
		msg = openai.AssistantMessage(m.Content)
		if includeReasoning && isSet(m.Reasoning) {
			msg.OfAssistant.SetExtraFields(map[string]any{ReasoningField: m.Reasoning})
		}
		if len(m.ToolCall) != 0 {
			err = json.Unmarshal(m.ToolCall, &msg.OfAssistant.ToolCalls)
		}
		return msg, err
	case "tool":
		return openai.ToolMessage(m.Content, m.ToolCallID), nil
	default:
		return msg, fmt.Errorf("invalid message role: %s", m.Role)
	}
}

// Create a new chat completion request with given config settings. Messages with Excluded set are omitted from the request.
// Includes reasoning content starting from the beginning of the latest turn.
func NewRequest(modelName string, conv Conversation, tools ...ToolFunction) (req openai.ChatCompletionNewParams, err error) {
	cfg := conv.Config
	extra := map[string]any{}
	req.Model = shared.ChatModel(modelName)
	req.ReasoningEffort = shared.ReasoningEffort(cfg.ReasoningEffort)
	req.Temperature = openai.Float(cfg.Temperature)
	if cfg.TopP != 0 {
		req.TopP = openai.Float(cfg.TopP)
	}
	if cfg.TopK != 0 {
		extra["top_k"] = cfg.TopK
	}
	if cfg.PresencePenalty != 0 {
		req.PresencePenalty = openai.Float(cfg.PresencePenalty)
	}
	if cfg.RepetitionPenalty != 0 {
		extra["repetition_penalty"] = cfg.RepetitionPenalty
	}
	if cfg.SystemPrompt != "" {
		req.Messages = append(req.Messages, parseSystemPrompt(cfg.SystemPrompt))
	}
	req.SetExtraFields(extra)

	var enabledTools []ToolFunction
	for _, tool := range tools {
		def := tool.Definition()
		if slices.ContainsFunc(cfg.Tools, func(t ToolConfig) bool { return t.Enabled && t.Name == def.Name }) {
			enabledTools = append(enabledTools, tool)
		}
	}
	req.Tools = ChatCompletionToolParams(enabledTools)
	reasoningFrom := conv.LastUserMessageNumber()
	for i, m := range conv.Messages {
		if !m.Excluded {
			msg, err := FromMessage(m, i >= reasoningFrom)
			if err != nil {
				return req, err
			}
			req.Messages = append(req.Messages, msg)
		}
	}
	return req, nil
}

func parseSystemPrompt(s string) openai.ChatCompletionMessageParamUnion {
	today := time.Now().Format("2 January 2006")
	s = strings.ReplaceAll(s, "{{today}}", today)
	return openai.SystemMessage(s)
}

func msec(n int) string {
	return (time.Duration(n) * time.Millisecond).String()
}

// Pretty print struct
func Pretty(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Error(err)
	}
	return string(data)
}
