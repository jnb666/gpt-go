package api_test

import (
	_ "embed"
	"encoding/json"
	"os"
	"testing"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/weather"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata.json
var testConversation []byte

func TestTokenizeSimple(t *testing.T) {
	tokenize(t, testMessages, nil, 50, 51200, true)
}

func TestTokenizeWithTools(t *testing.T) {
	tools := weather.Tools(os.Getenv("OWM_API_KEY"))
	tokenize(t, testMessagesWithTools, tools, 149, 51200, true)
}

func tokenize(t *testing.T, msgs []api.Message, tools []api.ToolFunction, expected, maxExpected int, debug bool) {
	if debug {
		log.SetLevel(log.TraceLevel)
	}
	server := api.GetServer()
	if server != api.LlamaCPP && server != api.VLLM {
		t.Log("LLM_SERVER not set to valid server name - skipping test")
		return
	}
	cfg := api.DefaultConfig(tools...)
	cfg.SystemPrompt = "You are a helpful assistant."
	conv := api.NewConversation(cfg)
	conv.Messages = append(conv.Messages, msgs...)

	client, err := api.NewClient(server)
	require.NoError(t, err)
	req := client.NewRequest(client.ModelName, conv, tools...)

	toks, err := api.Tokenize(server, client.BaseURL, req.Messages)
	require.NoError(t, err)
	t.Logf("token count=%d max_model_len=%d", toks, client.ContextLength)
	assert.Equal(t, expected, toks, "number of tokens")
	assert.GreaterOrEqual(t, client.ContextLength, maxExpected, "max model len")
}

func TestExcludeMessages(t *testing.T) {
	server := api.GetServer()
	if server != api.LlamaCPP && server != api.VLLM {
		t.Log("LLM_SERVER not set to valid server name - skipping test")
		return
	}
	client, err := api.NewClient(server)
	require.NoError(t, err)

	var conv api.Conversation
	err = json.Unmarshal(testConversation, &conv)
	require.NoError(t, err)
	conv.NumTokens = 13489
	checkNumMessages(t, conv.Messages, 31, 0)

	log.SetLevel(log.DebugLevel)
	err = client.CompactMessages(conv, 12800)
	require.NoError(t, err)

	checkNumMessages(t, conv.Messages, 31, 16)
}

func checkNumMessages(t *testing.T, msgs []api.Message, total, excluded int) {
	assert.Equal(t, total, len(msgs), "total")
	n := 0
	for _, msg := range msgs {
		if msg.Excluded {
			n++
		}
	}
	assert.Equal(t, excluded, n, "excluded")
}
