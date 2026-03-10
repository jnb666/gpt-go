package api_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/weather"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testMessages = []api.Message{
	{Role: "user", Content: "hello"},
	{Role: "assistant",
		Content:   "Hello! How can I help you today?",
		Reasoning: "The user is just saying \"hi\". This is a simple greeting, so I should respond in a friendly and helpful manner",
	},
	{Role: "user", Content: "How many r's are there in Strawberry?"},
}

var testMessagesWithTools = []api.Message{
	{Role: "user", Content: "What's the weather like in London today?"},
	{Role: "assistant",
		Reasoning: "The user is asking about the current weather in London. I need to use the get_current_weather function with the location parameter set to \"London,GB\" (using the ISO 3166 country code for Great Britain).",
		ToolCall:  json.RawMessage(`[{"id":"call_f5fc4884ea3348a9b38d3bf6","function":{"arguments":"{\"location\":\"London,GB\"}","name":"get_current_weather"},"type":"function"}]`),
	},
	{Role: "tool",
		Content:    "Current weather for London,GB: 9°C - mist, feels like 7°C, wind 3.6m/s",
		ToolCallID: "call_f5fc4884ea3348a9b38d3bf6",
	},
}

func TestRequestSimple(t *testing.T) {
	cfg := api.DefaultConfig()
	cfg.SystemPrompt = "You are a helpful assistant."
	conv := api.NewConversation(cfg)
	conv.Messages = append(conv.Messages, testMessages...)

	client, err := api.NewClient(api.VLLM)
	require.NoError(t, err)

	req := client.NewRequest("", conv)
	t.Log(api.Pretty(req))

	expect := map[string]any{
		"messages": []map[string]any{
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "Hello! How can I help you today?"},
			{"role": "user", "content": "How many r's are there in Strawberry?"},
		},
		"temperature":         cfg.Temperature,
		"top_p":               cfg.TopP,
		"top_k":               cfg.TopK,
		"presence_penalty":    cfg.PresencePenalty,
		"repetition_penalty":  cfg.RepetitionPenalty,
		"reasoning_effort":    cfg.ReasoningEffort,
		"parallel_tool_calls": api.ParallelToolCalls,
	}
	assert.JSONEq(t, toJSON(expect), toJSON(req))
}

func TestRequestWithTools(t *testing.T) {
	tools := weather.Tools(os.Getenv("OWM_API_KEY"))
	cfg := api.DefaultConfig(tools...)
	cfg.SystemPrompt = "You are a helpful assistant."

	conv := api.NewConversation(cfg)
	conv.Messages = append(conv.Messages, testMessagesWithTools...)

	client, err := api.NewClient(api.VLLM)
	require.NoError(t, err)

	req := client.NewRequest("", conv, tools...)
	t.Log(api.Pretty(req))

	expect := map[string]any{
		"messages": []map[string]any{
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "What's the weather like in London today?"},
			{"role": "assistant",
				"content":   "",
				"reasoning": "The user is asking about the current weather in London. I need to use the get_current_weather function with the location parameter set to \"London,GB\" (using the ISO 3166 country code for Great Britain).",
				"tool_calls": []map[string]any{{
					"id":       "call_f5fc4884ea3348a9b38d3bf6",
					"function": map[string]any{"arguments": "{\"location\":\"London,GB\"}", "name": "get_current_weather"},
					"type":     "function",
				}},
			},
			{"role": "tool", "content": "Current weather for London,GB: 9°C - mist, feels like 7°C, wind 3.6m/s", "tool_call_id": "call_f5fc4884ea3348a9b38d3bf6"},
		},
		"temperature":         cfg.Temperature,
		"top_p":               cfg.TopP,
		"top_k":               cfg.TopK,
		"presence_penalty":    cfg.PresencePenalty,
		"repetition_penalty":  cfg.RepetitionPenalty,
		"reasoning_effort":    cfg.ReasoningEffort,
		"parallel_tool_calls": api.ParallelToolCalls,
		"tools":               api.ChatCompletionToolParams(tools),
	}
	assert.JSONEq(t, toJSON(expect), toJSON(req))
}

func toJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}
