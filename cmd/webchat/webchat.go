package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/browser"
	"github.com/jnb666/gpt-go/api/tools/weather"
	"github.com/jnb666/gpt-go/markdown"
	"github.com/sashabaranov/go-openai"
	log "github.com/sirupsen/logrus"
)

const MaxConversations = 30

var DataDir = getDataDir()

//go:embed assets
var assets embed.FS

var upgrader websocket.Upgrader

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.Parse()
	if debug {
		log.SetLevel(log.DebugLevel)
	}

	mux := &http.ServeMux{}
	mux.Handle("/", fsHandler())

	log.Println("Serving website at http://localhost:8000")
	mux.HandleFunc("/websocket", websocketHandler)

	err := http.ListenAndServe(":8000", logRequestHandler(mux))
	if err != nil {
		log.Fatal(err)
	}
}

// handler for websocket connections
func websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("websocket upgrade: ", err)
		return
	}
	defer conn.Close()

	c := newConnection(conn)

	cfg := api.DefaultConfig(c.tools...)
	if c.browser != nil {
		cfg.ToolDescription = c.browser.Description()
	}

	err = loadJSON("config.json", &cfg)
	if err != nil {
		log.Error(err)
	}
	log.Debugf("initial config: %#v", cfg)
	conv := api.NewConversation(cfg)

	for {
		var req api.Request
		err = conn.ReadJSON(&req)
		if err != nil {
			log.Error("read message: ", err)
			return
		}
		switch req.Action {
		case "list":
			err = c.listChats(conv.ID)
		case "add":
			conv, err = c.addMessage(conv, req.Message)
		case "load":
			conv, err = c.loadChat(req.ID, cfg)
		case "delete":
			conv, err = c.deleteChat(req.ID, cfg)
		case "config":
			conv, err = c.configOptions(conv, &cfg, req.Config)
		default:
			err = fmt.Errorf("request %q not supported", req.Action)
		}
		if err != nil {
			log.Error(err)
		}
	}
}

// connection state for websocket
type Connection struct {
	conn     *websocket.Conn
	client   *openai.Client
	tools    []api.ToolFunction
	browser  *browser.Browser
	channel  string
	content  string
	analysis string
	sequence int
}

// init connection with openai client and tool functions
func newConnection(conn *websocket.Conn) *Connection {
	c := &Connection{
		conn:   conn,
		client: api.NewClient(),
	}
	if apiKey := os.Getenv("OWM_API_KEY"); apiKey != "" {
		c.tools = weather.Tools(apiKey)
	} else {
		log.Warn("skipping weather tools support - OWM_API_KEY env variable is not defined")
	}
	if apiKey := os.Getenv("BRAVE_API_KEY"); apiKey != "" {
		c.browser = &browser.Browser{BraveApiKey: apiKey}
		c.tools = append(c.tools, c.browser.Tools()...)
	} else {
		log.Warn("skipping browser tools support - BRAVE_API_KEY env variable is not defined")
	}
	return c
}

// get list of saved conversation ids and current model id
func (c *Connection) listChats(currentID string) error {
	log.Infof("list saved chats: current=%s", currentID)
	var err error
	resp := api.Response{Action: "list", Conversation: api.Conversation{ID: currentID}}
	resp.Model, err = modelName(c.client)
	if err != nil {
		return err
	}
	resp.List, err = getSavedConversations()
	if err != nil {
		return err
	}
	return c.conn.WriteJSON(resp)
}

// add new message from user to chat, get streaming response, returns updated message list
func (c *Connection) addMessage(conv api.Conversation, msg api.Message) (api.Conversation, error) {
	start := time.Now()
	log.Infof("add message: %q", msg.Content)
	conv.Messages = append(conv.Messages, msg)
	req := api.NewRequest(conv, c.tools...)
	req.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	c.channel = ""
	c.content = ""
	c.analysis = ""
	c.sequence = 0
	if c.browser != nil {
		c.browser.Reset()
	}
	_, usage, err := api.CreateChatCompletionStream(context.Background(), c.client, req, c.streamMessage, c.tools...)
	if err != nil {
		return conv, err
	}
	if c.browser != nil && len(c.browser.Docs) > 0 {
		c.content = c.browser.Postprocess(c.content)
	}
	err = c.sendUpdate("final", "\n", true)
	if err != nil {
		return conv, err
	}
	if c.analysis != "" {
		conv.Messages = append(conv.Messages, api.Message{Type: "analysis", Content: c.analysis})
	}
	conv.Messages = append(conv.Messages, api.Message{Type: "final", Content: c.content})
	err = saveJSON(conv.ID, conv)
	if err == nil && len(conv.Messages) <= 3 {
		err = c.listChats(conv.ID)
	}
	elapsed := time.Since(start).Round(time.Second)
	if usage != nil {
		log.Infof("Usage: prompt tokens=%d  reasoning tokens=%d  completion tokens=%d  elapsed=%s",
			usage.PromptTokens, usage.CompletionTokensDetails.ReasoningTokens, usage.CompletionTokens, elapsed)
	}
	return conv, err
}

// chat completion stream callback to send updates to front end
func (c *Connection) streamMessage(delta openai.ChatCompletionStreamChoiceDelta) error {
	if delta.Role == "tool" {
		log.Debug("tool response: ", delta.Content)
		return c.sendUpdate("analysis", `<pre><code class="tool-response">`+delta.Content+`</code></pre>`, false)
	}
	if delta.ReasoningContent != "" {
		return c.sendUpdate("analysis", delta.ReasoningContent, false)
	}
	if delta.Content != "" {
		return c.sendUpdate("final", delta.Content, false)
	}
	return nil
}

func (c *Connection) sendUpdate(channel, text string, end bool) error {
	if c.channel != channel {
		c.channel = channel
		c.content = ""
		c.sequence = 0
	}
	c.content += text
	if channel == "analysis" {
		c.analysis += text
	}
	// only render final markdown content when new line is generated
	if channel == "final" && !strings.Contains(text, "\n") {
		return nil
	}
	r := api.Response{Action: "add", Message: api.Message{Type: channel, Content: toHTML(c.content, channel), Update: c.sequence > 0, End: end}}
	c.sequence++
	return c.conn.WriteJSON(r)
}

// load conversation with given id, or new conversation if blank
func (c *Connection) loadChat(id string, cfg api.Config) (conv api.Conversation, err error) {
	log.Infof("load chat: id=%s", id)
	if id != "" {
		if err = loadJSON(id, &conv); err != nil {
			return conv, err
		}
		for _, tool := range cfg.Tools {
			if !slices.ContainsFunc(conv.Config.Tools, func(t api.ToolConfig) bool { return t.Name == tool.Name }) {
				conv.Config.Tools = append(conv.Config.Tools, api.ToolConfig{Name: tool.Name})
			}
		}

	} else {
		conv = api.NewConversation(cfg)
	}
	resp := api.Response{Action: "load", Conversation: api.Conversation{ID: conv.ID}}
	for _, msg := range conv.Messages {
		resp.Conversation.Messages = append(resp.Conversation.Messages, api.Message{Type: msg.Type, Content: toHTML(msg.Content, msg.Type)})
	}
	err = c.conn.WriteJSON(resp)
	return conv, err
}

// delete chat with given id and return new conversation
func (c *Connection) deleteChat(id string, cfg api.Config) (conv api.Conversation, err error) {
	log.Infof("delete conversation: id=%s", id)
	err = os.Remove(filepath.Join(DataDir, id+".json"))
	if err != nil {
		return conv, err
	}
	err = c.listChats(conv.ID)
	if err != nil {
		return conv, err
	}
	return c.loadChat("", cfg)
}

// if update is nil return current config settings, else update with provided values
func (c *Connection) configOptions(conv api.Conversation, cfg, update *api.Config) (api.Conversation, error) {
	var err error
	if update == nil {
		log.Info("get config")
		resp := api.Response{Action: "config", Config: conv.Config}
		err = c.conn.WriteJSON(resp)
	} else {
		if len(conv.Messages) == 0 {
			log.Infof("update default config: %#v", update)
			*cfg = *update
			err = saveJSON("config.json", cfg)
		} else {
			log.Infof("update config for current chat: %#v", update)
			conv.Config = *update
			err = saveJSON(conv.ID, conv)
		}
	}
	return conv, err
}

// current loaded model name
func modelName(client *openai.Client) (string, error) {
	resp, err := client.ListModels(context.Background())
	if err != nil {
		return "", err
	}
	if len(resp.Models) == 0 {
		return "", fmt.Errorf("model name not found")
	}
	return strings.TrimSuffix(filepath.Base(resp.Models[0].ID), ".gguf"), nil
}

// list of saved conversation files
func getSavedConversations() (list []api.Item, err error) {
	entries, err := os.ReadDir(DataDir)
	if err != nil {
		return nil, err
	}
	// results in most recent first
	for i := len(entries) - 1; i >= 0 && i >= len(entries)-MaxConversations; i-- {
		e := entries[i]
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".json") {
			data, err := os.ReadFile(filepath.Join(DataDir, e.Name()))
			if err != nil {
				return nil, err
			}
			var c api.Conversation
			if err = json.Unmarshal(data, &c); err != nil {
				return nil, err
			}
			if len(c.Messages) > 0 {
				list = append(list, api.Item{ID: c.ID, Summary: c.Messages[0].Content})
			}
		}
	}
	return list, nil
}

// handler to server static embedded files
func fsHandler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}

// handler to log http requests
func logRequestHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		log.Printf("%s: %s", r.Method, r.URL)
	})
}

// util functions
func toHTML(content, msgType string) string {
	if msgType == "final" || strings.Contains(content, "```") {
		html, err := markdown.Render(content)
		if err == nil {
			return html
		} else {
			log.Error("error converting markdown:", err)
		}
	}
	content = strings.ReplaceAll(content, "\n", "<br>")
	if msgType != "analysis" {
		return "<p>" + content + "</p>"
	}
	return content
}

func loadJSON(file string, v any) error {
	if !strings.HasSuffix(file, ".json") {
		file += ".json"
	}
	filename := filepath.Join(DataDir, file)
	data, err := os.ReadFile(filename)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, v)
	if err != nil {
		return err
	}
	log.Debugf("Loaded JSON from %s", filename)
	return nil
}

func saveJSON(file string, v any) error {
	if !strings.HasSuffix(file, ".json") {
		file += ".json"
	}
	filename := filepath.Join(DataDir, file)
	log.Debugf("Save JSON to %s", filename)
	data, err := json.Marshal(v)
	if err == nil {
		err = os.WriteFile(filename, data, 0644)
	}
	return err
}

func getDataDir() string {
	base, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	dir := filepath.Join(base, ".gpt-go")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		panic(err)
	}
	return dir
}
