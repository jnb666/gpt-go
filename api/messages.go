package api

import (
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/sashabaranov/go-openai"
)

var defaultConfig = Config{
	ModelIdentity:   "You are ChatGPT, a large language model trained by OpenAI.",
	ReasoningEffort: "medium",
}

// Chat API request from frontend to webserver
type Request struct {
	Action  string  `json:"action"`           // add | list | load | delete | config
	ID      string  `json:"id,omitzero"`      // if action=load,delete uuid format
	Message Message `json:"message,omitzero"` // if action=add
	Config  *Config `json:"config,omitzero"`  // if action=config
}

// Chat API response from webserver back to frontend
type Response struct {
	Action       string       `json:"action"`                // add | list | load | config
	Message      Message      `json:"message,omitzero"`      // if action=add
	Conversation Conversation `json:"conversation,omitzero"` // if action=load
	Model        string       `json:"model,omitzero"`        // if action=list
	List         []Item       `json:"list,omitzero"`         // if action=list
	Config       Config       `json:"config,omitzero"`       // if action=config
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
	ModelIdentity   string       `json:"model_identity"`
	ReasoningEffort string       `json:"reasoning_effort"` // low | medium | high
	Tools           []ToolConfig `json:"tools,omitzero"`
	ToolDescription string       `json:"tools_description,omitzero"`
}

type ToolConfig struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// Get default configuration with given tools enabled
func DefaultConfig(tools ...ToolFunction) Config {
	cfg := defaultConfig
	for _, tool := range tools {
		cfg.Tools = append(cfg.Tools, ToolConfig{Name: tool.Definition().Function.Name, Enabled: true})
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
func NewRequest(conv Conversation, tools ...ToolFunction) (req openai.ChatCompletionRequest) {
	cfg := conv.Config
	req.ChatTemplateKwargs = map[string]any{}
	if cfg.ModelIdentity != "" {
		req.ChatTemplateKwargs["model_identity"] = cfg.ModelIdentity
	}
	if cfg.ReasoningEffort != "" {
		req.ChatTemplateKwargs["reasoning_effort"] = cfg.ReasoningEffort
	}
	var system []string
	if cfg.SystemPrompt != "" {
		system = append(system, cfg.SystemPrompt)
	}
	if cfg.ToolDescription != "" {
		system = append(system, cfg.ToolDescription)
	}
	if len(system) > 0 {
		req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleDeveloper, Content: strings.Join(system, "\n\n")})
	}
	for _, tool := range tools {
		def := tool.Definition()
		if slices.ContainsFunc(cfg.Tools, func(t ToolConfig) bool { return t.Enabled && t.Name == def.Function.Name }) {
			req.Tools = append(req.Tools, def)
		}
	}
	for _, msg := range conv.Messages {
		if msg.Type == "user" {
			req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: msg.Content})
		} else if msg.Type == "final" {
			req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: msg.Content})
		}
	}
	return req
}
