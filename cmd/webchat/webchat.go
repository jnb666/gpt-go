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
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/browser"
	"github.com/jnb666/gpt-go/api/tools/python"
	"github.com/jnb666/gpt-go/api/tools/weather"
	"github.com/jnb666/gpt-go/markdown"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	log "github.com/sirupsen/logrus"
)

const MaxConversations = 30

var DataDir = getDataDir()

//go:embed assets
var assets embed.FS

var upgrader websocket.Upgrader

var debug, nostream bool
var apiServer = api.LlamaCpp

func main() {
	var server http.Server
	var openrouter bool
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.BoolVar(&api.Debug, "trace", false, "trace request and response messages")
	flag.BoolVar(&nostream, "nostream", false, "don't stream responses")
	flag.BoolVar(&openrouter, "openrouter", false, "use openrouter endpoint")
	flag.StringVar(&server.Addr, "server", ":8000", "web server address")
	flag.Parse()

	log.SetFormatter(&log.TextFormatter{ForceColors: true})
	if debug {
		log.SetLevel(log.DebugLevel)
	}
	if api.Debug {
		f, err := os.Create("debug.log")
		if err == nil {
			log.Info("writing debug trace to debug.log")
			api.DebugTo = f
		}
	}
	if openrouter {
		apiServer = api.OpenRouter
	}

	http.Handle("/", fsHandler())
	ctx, wsCancel := context.WithCancel(context.Background())
	http.HandleFunc("/websocket", websocketHandler(ctx))

	// launch web server in background
	go func() {
		addr := server.Addr
		if strings.HasPrefix(addr, ":") {
			host, _ := os.Hostname()
			addr = host + addr
		}
		log.Infof("Serving website at http://%s", addr)
		err := server.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("HTTP server error: ", err)
		}
	}()

	// shutdown cleanly on signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	wsCancel()
	time.Sleep(100 * time.Millisecond)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatal("HTTP shutdown error: ", err)
	}
	log.Info("server shutdown")
}

// connection state for websocket
type Connection struct {
	conn     *websocket.Conn
	client   openai.Client
	tools    []api.ToolFunction
	browser  *browser.Browser
	python   *python.Python
	content  string
	analysis string
	first    bool
}

type Message struct {
	req api.Request
	err error
}

// handler for websocket connections
func websocketHandler(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("websocket upgrade: ", err)
			return
		}
		defer conn.Close()

		baseURL, modelName := api.DefaultModel(apiServer)
		log.Infof("connecting to %s %s", baseURL, modelName)
		c := &Connection{
			conn:   conn,
			client: openai.NewClient(option.WithBaseURL(baseURL)),
		}
		c.browser, c.python, c.tools = initTools()
		defer c.browser.Close()
		defer c.python.Stop()

		cfg := api.DefaultConfig(c.tools...)
		err = loadJSON("config.json", &cfg)
		if err != nil {
			log.Error(err)
		}
		log.Debugf("initial config: %#v", cfg)

		err = c.handleWebsocket(ctx, cfg, modelName)
		if err != nil {
			log.Warn(err)
		} else {
			log.Info("close websocket")
		}
	}
}

func pollWebsocket(conn *websocket.Conn, ch chan Message) {
	for {
		var msg Message
		msg.err = conn.ReadJSON(&msg.req)
		ch <- msg
	}
}

func readMsg(ctx context.Context, ch chan Message) (req api.Request, err error) {
	for {
		select {
		case <-ctx.Done():
			return req, ctx.Err()
		case msg := <-ch:
			return msg.req, msg.err
		}
	}
}

func (c *Connection) handleWebsocket(ctx context.Context, cfg api.Config, model string) error {
	conv := api.NewConversation(cfg)
	ch := make(chan Message)
	go pollWebsocket(c.conn, ch)
	for {
		req, err := readMsg(ctx, ch)
		if err != nil {
			return err
		}
		switch req.Action {
		case "list":
			err = c.listChats(conv.ID)
		case "add":
			conv, err = c.addMessage(conv, req.Message, model)
		case "load":
			conv, err = c.loadChat(req.ID, cfg)
		case "delete":
			conv, err = c.deleteChat(req.ID, cfg)
		case "config":
			conv, err = c.configOptions(conv, &cfg, req.Config)
		default:
			return fmt.Errorf("request %q not supported", req.Action)
		}
		if err != nil {
			return err
		}
	}
}

// initialise supported tools
func initTools() (browse *browser.Browser, pyexec *python.Python, tools []api.ToolFunction) {
	pyexec = python.New()
	tools = []api.ToolFunction{pyexec}
	if apiKey := os.Getenv("BRAVE_API_KEY"); apiKey != "" {
		browse = browser.NewBrowser(apiKey)
		tools = append(tools, browse.Tools()...)
	} else {
		log.Warn("skipping browser tools support - BRAVE_API_KEY env variable is not defined")
	}
	if apiKey := os.Getenv("OWM_API_KEY"); apiKey != "" {
		tools = append(tools, weather.Tools(apiKey)...)
	} else {
		log.Warn("skipping weather tools support - OWM_API_KEY env variable is not defined")
	}
	return
}

// get list of saved conversation ids and current model id
func (c *Connection) listChats(currentID string) error {
	log.Infof("list saved chats: current=%s", currentID)
	var err error
	resp := api.Response{Action: "list", Conversation: api.Conversation{ID: currentID}}
	resp.List, err = getSavedConversations()
	if err != nil {
		return err
	}
	return c.conn.WriteJSON(resp)
}

// add new message from user to chat, get streaming response, returns updated message list
func (c *Connection) addMessage(conv api.Conversation, msg api.Message, model string) (api.Conversation, error) {
	log.Infof("add message: %q", msg.Content)
	conv.Messages = append(conv.Messages, msg)
	req := api.NewRequest(model, conv, c.tools...)
	c.content = ""
	c.analysis = ""
	c.first = true
	c.browser.Reset()
	c.python.Stop()

	var err error
	ctx := context.Background()
	if nostream {
		_, err = api.ChatCompletion(ctx, c.client, req, apiServer, c.sendUpdate, c.updateStats, c.tools...)
	} else {
		_, err = api.ChatCompletionStream(ctx, c.client, req, apiServer, c.sendUpdate, c.updateStats, c.tools...)
	}
	if err != nil {
		return conv, err
	}
	if c.browser != nil && len(c.browser.Docs) > 0 {
		c.content = c.browser.Postprocess(c.content)
	}
	c.sendUpdate("final", c.content, -1, true)
	if c.analysis != "" {
		conv.Messages = append(conv.Messages, api.Message{Type: "analysis", Content: c.analysis})
	}
	conv.Messages = append(conv.Messages, api.Message{Type: "final", Content: c.content})
	err = saveJSON(conv.ID, conv)
	if err == nil && len(conv.Messages) <= 3 {
		err = c.listChats(conv.ID)
	}
	return conv, err
}

// update stats after each complete request
func (c *Connection) updateStats(stats api.Stats) {
	stats.Loginfo()
	if err := c.conn.WriteJSON(api.Response{Action: "stats", Stats: stats}); err != nil {
		log.Error(err)
	}
}

// chat completion stream callback to send updates to front end
func (c *Connection) sendUpdate(channel, text string, index int, end bool) {
	r := api.Response{Action: "add"}
	switch channel {
	case "analysis":
		r.Message.Type = "analysis"
		r.Message.Update = c.analysis != ""
		c.analysis += text
		r.Message.Content = c.analysis
	case "tool":
		r.Message.Type = "analysis"
		r.Message.Update = c.analysis != ""
		c.analysis += `<pre><code class="tool-response">` + text + `</code></pre>`
		r.Message.Content = c.analysis
	case "final":
		if end {
			// always complete message
			c.content = text
		} else {
			c.content += text
			// only render final markdown content when new line is generated
			if !strings.Contains(text, "\n") {
				return
			}
		}
		r.Message.Update = !c.first
		r.Message.Content = toHTML(c.content, "final")
		r.Message.End = end
		c.first = false
	default:
		log.Errorf("invalid channel %q", channel)
		return
	}
	if err := c.conn.WriteJSON(r); err != nil {
		log.Error(err)
	}
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
