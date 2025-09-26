// Python tool to execute code in a docker container
package python

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	api "github.com/docker/docker/api/types/container"
	"github.com/docker/go-sdk/container"
	"github.com/docker/go-sdk/container/exec"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/shared"
	log "github.com/sirupsen/logrus"
)

var DefaultConfig = Config{TimeSeconds: 120, MemoryBytes: 1024 * 1024 * 1024, OutputBytes: 10000}

// Python tool - implements the api.ToolFunction interface
type Python struct {
	ctr *container.Container
	cfg Config
}

// Limits for python code execution.
type Config struct {
	TimeSeconds int
	MemoryBytes int
	OutputBytes int
}

// Create a new python tool. Uses DefaultConfig if config is omitted.
func New(config ...Config) *Python {
	c := &Python{cfg: DefaultConfig}
	if len(config) > 0 {
		c.cfg = config[0]
	}
	return c
}

// Provide definition for model prompt
func (c *Python) Definition() shared.FunctionDefinitionParam {
	return shared.FunctionDefinitionParam{
		Name:   "python",
		Strict: openai.Bool(true),
		Description: openai.String("## python\n\n" +
			"// Use this tool to execute Python code in your chain of thought. The code will not be shown to the user.\n" +
			"// When you send a message containing Python code to python, it will be executed in a container environment. python will respond with the standard output and error\n" +
			"// or time out after 120.0 seconds. The current directory can be used to save and persist user files. Internet access for this session is blocked."),
		Parameters: shared.FunctionParameters{
			"type": "string",
		},
	}
}

// Stop current container if running
func (c *Python) Stop() {
	if c != nil && c.ctr != nil {
		log.Debug("python: stop container")
		if err := c.ctr.Terminate(context.Background(), container.TerminateTimeout(0)); err != nil {
			log.Error(err)
		}
		c.ctr = nil
	}
}

// Execute python code within container with time limit
func (c *Python) Call(input string) (code, resp string, err error) {
	var value any
	if err := json.Unmarshal([]byte(input), &value); err == nil {
		code = decodeArgs(value)
	}
	if code == "" {
		log.Infof("python code:\n%s", input)
		return code, "", fmt.Errorf("Error: invalid argument syntax - code should be a JSON encoded string")
	}
	log.Infof("python code:\n%s", code)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if c.ctr == nil {
		if err := c.start(ctx); err != nil {
			return code, "", err
		}
	}

	b := new(bytes.Buffer)
	err = c.exec(ctx, strconv.Quote(code), b)
	if err != nil {
		return code, "", err
	}
	log.Infof("python response:\n%s", b.String())
	return code, b.String(), nil
}

func (c *Python) start(ctx context.Context) error {
	var err error
	log.Debug("python: start container")
	c.ctr, err = container.Run(ctx,
		container.WithImage("gpt-go-python-tool"),
		container.WithHostConfigModifier(func(h *api.HostConfig) {
			h.NetworkMode = "none"
			h.Resources.Memory = int64(c.cfg.MemoryBytes)
		}),
	)
	return err
}

func (c *Python) exec(ctx context.Context, code string, b *bytes.Buffer) error {
	timedOut := false
	ch := time.After(time.Duration(c.cfg.TimeSeconds) * time.Second)
	go func() {
		select {
		case <-ch:
			log.Debugf("python: command timed out - killing")
			timedOut = true
			c.ctr.Exec(ctx, []string{"killall", "python"})
		case <-ctx.Done():
		}
	}()

	rc, out, err := c.ctr.Exec(ctx, []string{"python", "-u", "/home/app/runner.py"},
		exec.WithEnv([]string{"USER_CODE=" + code}), exec.Multiplexed(),
	)
	if err != nil {
		return err
	}
	log.Debugf("python: exec rc = %d", rc)
	io.Copy(b, out)

	if b.Len() > c.cfg.OutputBytes {
		b.Truncate(c.cfg.OutputBytes)
		b.WriteString("\n=== output truncated ===\n")
	}
	if timedOut {
		b.WriteString("\nError: timed out - killed\n")
	} else if rc != 0 && rc != 1 {
		b.WriteString("\nError: execution failed\n")
	}
	return nil
}

func decodeArgs(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		if code, ok := v["code"]; ok {
			return decodeArgs(code)
		}
	}
	return ""
}
