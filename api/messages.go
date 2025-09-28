package api

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/shared"
	log "github.com/sirupsen/logrus"
)

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
	Type    string `json:"type"`   // user | analysis | final
	Update  bool   `json:"update"` // true if update to existing message
	End     bool   `json:"end"`    // true if update and message is now complete
	Content string `json:"content"`
}

type Item struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

type Config struct {
	SystemPrompt    string       `json:"system_prompt"`
	ReasoningEffort string       `json:"reasoning_effort"` // low | medium | high
	Tools           []ToolConfig `json:"tools,omitzero"`
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

func (s *Stats) Loginfo() {
	log.Infof("%d API calls in %s  %d prompt tokens  %d completion tokens at %.1f tok/sec",
		s.ApiCalls, msec(s.ApiTime), s.PromptTokens, s.CompletionTokens, s.CompletionTokensPerSec())
	if s.ToolCalls > 0 {
		funcs := fmt.Sprint(s.Functions)
		log.Infof("%d tool calls in %s - %s", s.ToolCalls, msec(s.ToolTime), funcs[4:len(funcs)-1])
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
	cfg := Config{ReasoningEffort: "medium"}
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

// Create a new chat completion request with given config settings.
func NewRequest(modelName string, conv Conversation, tools ...ToolFunction) (req openai.ChatCompletionNewParams) {
	cfg := conv.Config
	req.Model = shared.ChatModel(modelName)
	req.ReasoningEffort = shared.ReasoningEffort(cfg.ReasoningEffort)
	if cfg.SystemPrompt != "" {
		req.Messages = append(req.Messages, openai.DeveloperMessage(cfg.SystemPrompt))
	}
	var enabledTools []ToolFunction
	for _, tool := range tools {
		def := tool.Definition()
		if slices.ContainsFunc(cfg.Tools, func(t ToolConfig) bool { return t.Enabled && t.Name == def.Name }) {
			enabledTools = append(enabledTools, tool)
		}
	}
	req.Tools = ChatCompletionToolParams(enabledTools)
	for _, msg := range conv.Messages {
		if msg.Type == "user" {
			req.Messages = append(req.Messages, openai.UserMessage(msg.Content))
		} else if msg.Type == "final" {
			req.Messages = append(req.Messages, openai.AssistantMessage(msg.Content))
		}
	}
	return req
}

func msec(n int) string {
	return (time.Duration(n) * time.Millisecond).String()
}
