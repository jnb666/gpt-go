package api_test

import (
	_ "embed"
	"encoding/json"
	"os"
	"testing"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/browser"
	"github.com/jnb666/gpt-go/api/tools/weather"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata.json
var testConversation []byte

func TestTokenizeSimple(t *testing.T) {
	tokenize(t, testMessages, nil, 75, false)
}

func TestTokenizeWithTools(t *testing.T) {
	tools := weather.Tools(os.Getenv("OWM_API_KEY"))
	tokenize(t, testMessagesWithTools, tools, 636, false)
}

func tokenize(t *testing.T, msgs []api.Message, tools []api.ToolFunction, expected int, debug bool) {
	if debug {
		log.SetLevel(log.TraceLevel)
	}
	conv := api.NewConversation(api.DefaultConfig(tools...))
	conv.Messages = append(conv.Messages, msgs...)

	server := api.VLLM
	baseURL, modelName := api.DefaultModel(server)
	req, err := api.NewRequest(modelName, conv, tools...)
	require.NoError(t, err)

	toks, maxToks, err := api.Tokenize(server, baseURL, req)
	require.NoError(t, err)
	t.Logf("token count=%d max_model_len=%d", toks, maxToks)
	assert.Equal(t, expected, toks, "number of tokens")
	assert.Equal(t, maxToks, 40960, "max model len")
}

func TestExcludeMessages(t *testing.T) {
	var conv api.Conversation
	err := json.Unmarshal(testConversation, &conv)
	require.NoError(t, err)
	checkNumMessages(t, conv.Messages, 31, 0)

	browse := browser.NewBrowser(os.Getenv("BRAVE_API_KEY"))
	defer browse.Close()

	server := api.VLLM
	baseURL, modelName := api.DefaultModel(server)
	log.SetLevel(log.DebugLevel)
	req, err := api.CompactMessages(server, baseURL, modelName, conv, browse.Tools(), 0.25)
	require.NoError(t, err)
	assert.Equal(t, 16, len(req.Messages))
	checkNumMessages(t, conv.Messages, 31, 16)
	t.Log(api.Pretty(req.Messages))
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
