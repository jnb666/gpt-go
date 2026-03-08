package api

import (
	"fmt"
	"strings"

	"github.com/jnb666/gpt-go/api/tools"
	"github.com/openai/openai-go/v3"
	log "github.com/sirupsen/logrus"
)

// Estimate number of prompt tokens generated from the given list of messages.
// If this exceeds the maximum content length for the given model multiplied by the threshold factor
// then the oldest messages in the conversation are marked as excluded.
func CompactMessages(server Server, baseURL, modelName string, conv Conversation, tools []ToolFunction, threshold float64) (req openai.ChatCompletionNewParams, err error) {
	for {
		req, err := NewRequest(modelName, conv, tools...)
		if err != nil {
			return req, err
		}
		tokens, maxTokens, err := Tokenize(server, baseURL, req)
		if err != nil {
			return req, err
		}
		limit := int(float64(maxTokens) * threshold)
		if tokens < limit {
			log.Infof("number of prompt tokens = %d / %d", tokens, limit)
			return req, nil
		}
		log.Warnf("number of prompt tokens %d exceeds threshold of %d - excluding old messages", tokens, limit)
		end := conv.LastUserMessageNumber()
		for i := 0; i < end; i++ {
			msg := conv.Messages[i]
			if !msg.Excluded && msg.Role == "user" {
				if excludeTurnAt(i, end, conv.Messages) {
					break
				}
				return req, fmt.Errorf("ExcludeOldMessages: exceeded limit but no more messages to exclude!")
			}
		}
	}
}

func excludeTurnAt(start, end int, msgs []Message) bool {
	msgs[start].Excluded = true
	for i := start + 1; i < end; i++ {
		if msgs[i].Role == "user" {
			log.Infof("excluded %d messages from turn starting at message %d", i-start, start)
			return true
		}
		msgs[i].Excluded = true
	}
	return false
}

// Convert text to tokens for the given model, or default if modelName is empty - vLLM specific
func Tokenize(server Server, baseURL string, req openai.ChatCompletionNewParams) (numTokens, maxModelLen int, err error) {
	switch server {
	case VLLM:
		return tokenizeVLLM(baseURL, req)
	default:
		return 0, 0, fmt.Errorf("Tokenize only implemented for VLLM")
	}
}

func tokenizeVLLM(baseURL string, req openai.ChatCompletionNewParams) (numTokens, maxModelLen int, err error) {
	type Request struct {
		Model    string                                   `json:"model"`
		Messages []openai.ChatCompletionMessageParamUnion `json:"messages"`
		Tools    []openai.ChatCompletionToolUnionParam    `json:"tools,omitzero"`
	}
	type Response struct {
		Count       int `json:"count"`
		MaxModelLen int `json:"max_model_len"`
	}
	url := strings.TrimSuffix(baseURL, "/v1") + "/tokenize"
	var resp Response
	request := Request{Model: req.Model, Messages: req.Messages, Tools: req.Tools}
	if log.GetLevel() >= log.TraceLevel {
		log.Trace(Pretty(request))
	}
	_, err = tools.Post(url, request, &resp)
	if err != nil {
		return 0, 0, err
	}
	return resp.Count, resp.MaxModelLen, nil
}
