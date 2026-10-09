package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Ollama speaks the Messages API; sessions reuse the existing Claude CLI.
type ollamaConfig struct {
	model, baseURL string
	contextTokens  int
}

func newOllamaConfig(model, host string, contextTokens int) (*ollamaConfig, error) {
	model, host = strings.TrimSpace(model), strings.TrimSpace(host)
	if model == "" || contextTokens <= 0 {
		return nil, fmt.Errorf("Ollama needs a model and a positive -ollama-context")
	}
	if host == "" {
		host = "http://127.0.0.1:11434"
	} else if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("OLLAMA_HOST must be an HTTP(S) base URL or host:port")
	}
	return &ollamaConfig{model: model, baseURL: strings.TrimSuffix(u.String(), "/"), contextTokens: contextTokens}, nil
}

func (c *ollamaConfig) apiBackend() apiBackend {
	return apiBackend{
		client: anthropic.NewClient(option.WithoutEnvironmentDefaults(),
			option.WithBaseURL(c.baseURL), option.WithAPIKey("ollama")),
		model: anthropic.Model(c.model),
		local: true,
	}
}

// Override credentials and provider settings only in the local session child.
func (c *ollamaConfig) configure(cmd *exec.Cmd) {
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key) // Environment keys are case-insensitive on Windows.
		if !strings.HasPrefix(key, "ANTHROPIC_") &&
			(!strings.HasPrefix(key, "CLAUDE_CODE_") || key == "CLAUDE_CODE_GIT_BASH_PATH") &&
			key != "MAX_THINKING_TOKENS" && key != "DISABLE_TELEMETRY" &&
			key != "DISABLE_ERROR_REPORTING" && key != "DISABLE_UPDATES" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env,
		"ANTHROPIC_API_KEY=ollama", "ANTHROPIC_BASE_URL="+c.baseURL,
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS="+strconv.Itoa(c.contextTokens),
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1",
		"DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1", "DISABLE_UPDATES=1",
	)
}
