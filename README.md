# gpt-go

[Package documentation](https://pkg.go.dev/github.com/jnb666/gpt-go)

Code to interact with LLM models using the OpenAI chat completions API.

Supports tool calling and includes some example tools:
- [weather](/jnb666/gpt-go/tree/main/api/tools/weather) : to get weather data via openweathermap
- [browser](/jnb666/gpt-go/tree/main/api/tools/browser) : to search the web using the Brave search API and extract pages with Playwright
- [python](/jnb666/gpt-go/tree/main/api/tools/python) : to execute python code in a Docker container

Some example programs:
- [chat](/jnb666/gpt-go/tree/main/cmd/chat) : simple command line chat example
- [tools](/jnb666/gpt-go/tree/main/cmd/tools) : as above but with tool calling
- [webchat](/jnb666/gpt-go/tree/main/cmd/webchat) : web based front end

See the blog posts at [itsabanana.dev](https://itsabanana.dev/posts) for some background.
