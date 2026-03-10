# gpt-go

[Package documentation](https://pkg.go.dev/github.com/jnb666/gpt-go)

Code to interact with LLM models using the OpenAI chat completions API.

Supports tool calling and includes some example tools:
- [weather](https://github.com/jnb666/gpt-go/tree/main/api/tools/weather) : to get weather data via openweathermap
- [browser](https://github.com/jnb666/gpt-go/tree/main/api/tools/browser) : to search the web using the Brave search API and extract pages with Playwright
- [python](https://github.com/jnb666/gpt-go/tree/main/api/tools/python) : to execute python code in a Docker container

Some example programs:
- [chat](https://github.com/jnb666/gpt-go/tree/main/cmd/chat) : simple command line chat example
- [tools](https://github.com/jnb666/gpt-go/tree/main/cmd/tools) : as above but with tool calling
- [webchat](https://github.com/jnb666/gpt-go/tree/main/cmd/webchat) : web based front end

