// Command line chat example with tool calling
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools/weather"
	"github.com/sashabaranov/go-openai"
)

var apiKey = os.Getenv("OWM_API_KEY")

func main() {
	log.SetFlags(0)

	client := api.NewClient()

	currentWeather := weather.Current{ApiKey: apiKey}
	weatherForecast := weather.Forecast{ApiKey: apiKey}

	req := openai.ChatCompletionRequest{
		Tools: []openai.Tool{currentWeather.Definition(), weatherForecast.Definition()},
	}
	input := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		question, err := input.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}
		req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: "user", Content: question})

		resp, _, err := api.CreateChatCompletionStream(context.Background(), client, req,
			printOutput(), currentWeather, weatherForecast)
		if err != nil {
			log.Fatal(err)

		}
		fmt.Println()
		req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: "assistant", Content: resp.Message.Content})
	}
}

// Print output from chat completion stream to stdout
func printOutput() func(openai.ChatCompletionStreamChoiceDelta) error {
	channel := ""
	return func(delta openai.ChatCompletionStreamChoiceDelta) error {
		if delta.Role == "tool" {
			fmt.Printf("\n## tool response\n%s\n", delta.Content)
			return nil
		}
		if delta.ReasoningContent != "" {
			if channel == "" {
				fmt.Println("## analysis")
				channel = "analysis"
			}
			fmt.Print(delta.ReasoningContent)
		}
		if delta.Content != "" {
			if channel == "analysis" {
				fmt.Println("\n\n## final")
				channel = "final"
			}
			fmt.Print(delta.Content)
		}
		if len(delta.ToolCalls) > 0 && delta.ToolCalls[0].Function.Name != "" {
			fmt.Println("\n\n## tool call")
			return nil
		}
		return nil
	}
}
