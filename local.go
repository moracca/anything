package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// localConfig is a local server that speaks the Anthropic Messages API, such
// as Ollama or oMLX. Ordinary requests call it directly; sessions run the
// Claude CLI against it.
type localConfig struct {
	name           string // for the banner and errors
	model, baseURL string
	apiKey         string
	contextTokens  int
}

func newLocalConfig(name, hostVar, model, host, defaultHost, apiKey string, contextTokens int) (*localConfig, error) {
	model, host = strings.TrimSpace(model), strings.TrimSpace(host)
	if model == "" || contextTokens <= 0 {
		return nil, fmt.Errorf("%s needs a model and a positive context budget", name)
	}
	if host == "" {
		host = defaultHost
	} else if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%s must be an HTTP(S) base URL or host:port", hostVar)
	}
	return &localConfig{name: name, model: model, baseURL: strings.TrimSuffix(u.String(), "/"),
		apiKey: apiKey, contextTokens: contextTokens}, nil
}

const defaultOllamaContext = 65536

// Ollama needs no key; the SDK and Claude CLI still want one, so send a placeholder.
func newOllamaConfig(model, host string, contextTokens int) (*localConfig, error) {
	return newLocalConfig("Ollama", "OLLAMA_HOST", model, host, "http://127.0.0.1:11434", "ollama", contextTokens)
}

// omlxSettings is what anything needs from oMLX's own settings file.
type omlxSettings struct {
	Server struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	} `json:"server"`
	Auth struct {
		APIKey string `json:"api_key"`
	} `json:"auth"`
	Sampling struct {
		MaxContextWindow int `json:"max_context_window"`
	} `json:"sampling"`
}

// readOMLXSettings reads ~/.omlx/settings.json; a missing or unreadable file
// just means falling back to oMLX's defaults.
func readOMLXSettings() (s omlxSettings) {
	home, err := os.UserHomeDir()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(filepath.Join(home, ".omlx", "settings.json"))
	if err != nil {
		return s
	}
	json.Unmarshal(data, &s)
	return s
}

// newOMLXConfig fills whatever the environment and flags leave unset (host,
// key, context budget: zero) from oMLX's settings file, then oMLX's defaults.
func newOMLXConfig(model, host, apiKey string, contextTokens int, settings omlxSettings) (*localConfig, error) {
	if host == "" && settings.Server.Port > 0 {
		h := settings.Server.Host
		if h == "" || h == "0.0.0.0" || h == "::" {
			h = "127.0.0.1"
		}
		host = "http://" + h + ":" + strconv.Itoa(settings.Server.Port)
	}
	if apiKey == "" {
		apiKey = settings.Auth.APIKey
	}
	if apiKey == "" {
		apiKey = "omlx" // accepted when oMLX skips key verification on localhost
	}
	if contextTokens == 0 {
		contextTokens = settings.Sampling.MaxContextWindow
	}
	if contextTokens == 0 {
		contextTokens = 32768
	}
	return newLocalConfig("oMLX", "OMLX_HOST", model, host, "http://127.0.0.1:8000", apiKey, contextTokens)
}

func (c *localConfig) apiBackend() apiBackend {
	return apiBackend{
		client: anthropic.NewClient(option.WithoutEnvironmentDefaults(),
			option.WithBaseURL(c.baseURL), option.WithAPIKey(c.apiKey)),
		model: anthropic.Model(c.model),
		local: true,
	}
}

// Override credentials and provider settings only in the local session child.
func (c *localConfig) configure(cmd *exec.Cmd) {
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
		// Claude Code 2.1.296 in -p --bare mode answers "Not logged in" when
		// given ANTHROPIC_API_KEY for a custom base URL, but accepts the same
		// key as a bearer token. Ollama ignores it; oMLX accepts either header.
		"ANTHROPIC_AUTH_TOKEN="+c.apiKey, "ANTHROPIC_BASE_URL="+c.baseURL,
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS="+strconv.Itoa(c.contextTokens),
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1",
		"DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1", "DISABLE_UPDATES=1",
	)
}
