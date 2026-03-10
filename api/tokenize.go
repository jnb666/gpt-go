package api

import (
	"fmt"
	"strings"

	"github.com/jnb666/gpt-go/api/tools"
	"github.com/openai/openai-go/v3"
	log "github.com/sirupsen/logrus"
)

// Estimate number of prompt tokens generated from the given list of messages.
// If this exceeds the given limit then the oldest messages in the conversation are marked as excluded.
func (c *Client) CompactMessages(conv Conversation, limit int) error {
	if c.Server != LlamaCPP && c.Server != VLLM || c.ContextLength == 0 {
		log.Warnf("CompactMessages: skipping as not supported for %s server", c.Server)
		return nil
	}
	// calc additional tokens in latest user message
	n := len(conv.Messages)
	if n == 0 {
		log.Warn("empty conversation passed to CompactMessages - skipping")
		return nil
	}
	newTokens, err := Tokenize(c.Server, c.BaseURL, []openai.ChatCompletionMessageParamUnion{FromMessage(conv.Messages[n-1], "")})
	if err != nil {
		return err
	}
	tokens := conv.NumTokens + newTokens
	end := conv.LastUserMessageNumber()
	for {
		if tokens <= limit {
			log.Infof("number of prompt tokens = %d / %d", tokens, limit)
			return nil
		}
		log.Warnf("number of prompt tokens %d exceeds threshold of %d - excluding old messages", tokens, limit)
		for i := 0; i < end; i++ {
			msg := conv.Messages[i]
			if !msg.Excluded && msg.Role == "user" {
				excluded, err := excludeTurnAt(i, end, conv.Messages)
				if err != nil {
					return err
				}
				excudedTokens, err := Tokenize(c.Server, c.BaseURL, excluded)
				if err != nil {
					return err
				}
				tokens -= excudedTokens
				break
			}
		}
	}
}

func excludeTurnAt(start, end int, msgs []Message) (excluded []openai.ChatCompletionMessageParamUnion, err error) {
	msgs[start].Excluded = true
	excluded = append(excluded, FromMessage(msgs[start], ""))
	for i := start + 1; i < end; i++ {
		if msgs[i].Role == "user" {
			// llama.cpp gives "Assistant response prefill is incompatible with enable_thinking." error if last message is assistant so add a dummy record
			excluded = append(excluded, openai.UserMessage(""))
			log.Infof("excluded %d messages from turn starting at message %d", i-start, start)
			//log.Debug(Pretty(excluded))
			return excluded, nil
		}
		msgs[i].Excluded = true
		excluded = append(excluded, FromMessage(msgs[i], ""))
	}
	return nil, fmt.Errorf("ExcludeOldMessages: exceeded limit but no more messages to exclude")
}

// Get max content length for current model
func MaxModelLength(server Server, baseURL string) (maxLen int, err error) {
	switch server {
	case LlamaCPP:
		return maxModelLenLllamaCPP(baseURL)
	case VLLM:
		_, maxLen, err = tokenizeVLLM(baseURL, []openai.ChatCompletionMessageParamUnion{openai.UserMessage("test")})
		return
	default:
		return 0, fmt.Errorf("MaxModelLength not implemented for %s", server)
	}
}

func maxModelLenLllamaCPP(baseURL string) (int, error) {
	type Response struct {
		ModelAlias                string `json:"model_alias"`
		DefaultGenerationSettings struct {
			ContextSize int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	url := strings.TrimSuffix(baseURL, "/v1") + "/props"
	var resp Response
	_, err := tools.Get(url, &resp)
	if err != nil {
		return 0, err
	}
	//log.Debug(Pretty(resp))
	return resp.DefaultGenerationSettings.ContextSize, nil
}

// Convert text to tokens for current model
func Tokenize(server Server, baseURL string, messages []openai.ChatCompletionMessageParamUnion) (numTokens int, err error) {
	switch server {
	case LlamaCPP:
		return tokenizeLlamaCPP(baseURL, messages)
	case VLLM:
		numTokens, _, err = tokenizeVLLM(baseURL, messages)
		return
	default:
		return 0, fmt.Errorf("Tokenize not implemented for %s", server)
	}
}

func tokenizeLlamaCPP(baseURL string, messages []openai.ChatCompletionMessageParamUnion) (numTokens int, err error) {
	type TemplateRequest struct {
		Messages []openai.ChatCompletionMessageParamUnion `json:"messages"`
	}
	type TemplateResponse struct {
		Prompt string `json:"prompt"`
	}
	type TokenizeRequest struct {
		Content string `json:"content"`
	}
	type TokenizeResponse struct {
		Tokens []int `json:"tokens"`
	}

	url := strings.TrimSuffix(baseURL, "/v1") + "/apply-template"
	req := TemplateRequest{Messages: messages}
	//log.Debug(Pretty(req))
	var resp1 TemplateResponse
	_, err = tools.Post(url, req, &resp1)
	if err != nil {
		return 0, err
	}
	//log.Debug(Pretty(resp1))
	url = strings.TrimSuffix(baseURL, "/v1") + "/tokenize"
	var resp2 TokenizeResponse
	_, err = tools.Post(url, TokenizeRequest{Content: resp1.Prompt}, &resp2)
	if err != nil {
		return 0, err
	}
	return len(resp2.Tokens), nil
}

func tokenizeVLLM(baseURL string, messages []openai.ChatCompletionMessageParamUnion) (numTokens, maxModelLen int, err error) {
	type Request struct {
		Messages []openai.ChatCompletionMessageParamUnion `json:"messages"`
	}
	type Response struct {
		Count       int `json:"count"`
		MaxModelLen int `json:"max_model_len"`
	}
	url := strings.TrimSuffix(baseURL, "/v1") + "/tokenize"
	var resp Response
	request := Request{Messages: messages}
	_, err = tools.Post(url, request, &resp)
	if err != nil {
		return 0, 0, err
	}
	return resp.Count, resp.MaxModelLen, nil
}
