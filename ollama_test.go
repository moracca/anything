package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestOllamaStream(t *testing.T) {
	originalTheme := currentTheme()
	t.Cleanup(func() { setTheme(originalTheme) })
	t.Setenv("ANTHROPIC_API_KEY", "operator-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "operator-token")
	t.Setenv("ANTHROPIC_PROFILE", "operator-profile")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	event := func(kind, data string) string { return "event: " + kind + "\ndata: " + data + "\n\n" }
	// A Claude-shaped response model catches accidental API pricing for local calls.
	start := event("message_start", `{"type":"message_start","message":{"id":"msg_local","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"usage":{"input_tokens":17,"output_tokens":0}}}`)
	text := event("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		event("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"<!doctype html><title>Local</title>"}}`)
	finish := event("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		event("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":23}}`) +
		event("message_stop", `{"type":"message_stop"}`)
	for _, tc := range []struct {
		name, body, theme string
		status            int
		wantErr           bool
	}{
		{"complete", start + text + finish, "", http.StatusOK, false},
		{"theme", start + text + finish, "deep sea research station, 1970s", http.StatusOK, false},
		{"incomplete", start + text, "", http.StatusOK, true},
		{"malformed block", start + event("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"bad"}}`), "", http.StatusOK, true},
		{"stream error", event("error", `{"type":"error","error":{"type":"overloaded_error","message":"local stream failed"}}`), "", http.StatusOK, true},
		{"HTTP error", `{"type":"error","error":{"type":"invalid_request_error","message":"local request failed"}}`, "", http.StatusBadRequest, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTheme(tc.theme)
			type captured struct {
				path, method string
				headers      http.Header
				body         []byte
			}
			requests := make(chan captured, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
				}
				requests <- captured{r.URL.Path, r.Method, r.Header.Clone(), body}
				w.Header().Set("Content-Type", "text/event-stream")
				if tc.status != http.StatusOK {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			config, err := newOllamaConfig("qwen3.8:27b", server.URL, 65536)
			if err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			stats, err := generateAPI(context.Background(), config.apiBackend(), "raw request", func(s string) { output.WriteString(s) })
			if (err != nil) != tc.wantErr {
				t.Fatalf("generation error = %v, want error %v", err, tc.wantErr)
			}
			request := <-requests
			if request.method != http.MethodPost || request.path != "/v1/messages" {
				t.Fatalf("unexpected endpoint: %s %s", request.method, request.path)
			}
			if request.headers.Get("X-Api-Key") != "ollama" || request.headers.Get("Authorization") != "" {
				t.Error("local request did not isolate operator credentials")
			}
			if strings.Contains(request.headers.Get("Anthropic-Beta"), "server-side-fallback") {
				t.Error("local request enables Anthropic server-side fallback")
			}
			var params struct {
				Model        string
				Stream       bool
				MaxTokens    int64 `json:"max_tokens"`
				Thinking     struct{ Type string }
				Fallbacks    json.RawMessage
				OutputConfig json.RawMessage `json:"output_config"`
				System       []struct{ Text string }
				Messages     []struct {
					Role    string
					Content []struct{ Type, Text string }
				}
			}
			if err := json.Unmarshal(request.body, &params); err != nil {
				t.Fatal(err)
			}
			if params.Model != config.model || !params.Stream || params.MaxTokens <= 0 || params.Thinking.Type != "disabled" {
				t.Errorf("incorrect local generation controls: %s", request.body)
			}
			if len(params.Fallbacks) != 0 || len(params.OutputConfig) != 0 {
				t.Error("local request contains Anthropic-specific fallback or effort settings")
			}
			if len(params.System) != 1 || params.System[0].Text != sitePrompt(false) || len(params.Messages) != 1 ||
				params.Messages[0].Role != "user" || len(params.Messages[0].Content) != 1 || params.Messages[0].Content[0].Text != "raw request" {
				t.Error("local request changed the application's prompts")
			}
			if !tc.wantErr {
				if output.String() != "<!doctype html><title>Local</title>" || stats.InputTokens != 17 || stats.OutputTokens != 23 || stats.StopReason != "end_turn" {
					t.Errorf("incorrect streamed output or usage: output=%q stats=%+v", output.String(), stats)
				}
				if stats.CostUSD != -1 || stats.Billed {
					t.Errorf("local generation reported an API charge: %+v", stats)
				}
			}
		})
	}
}

func TestOllamaConfiguration(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{"", "http://127.0.0.1:11434"},
		{"localhost:11434", "http://localhost:11434"},
		{"https://ollama.example/", "https://ollama.example"},
	} {
		config, err := newOllamaConfig("qwen3.8:27b", tc.host, 65536)
		if err != nil || config.baseURL != tc.want {
			t.Errorf("host %q: config=%+v error=%v, want %q", tc.host, config, err, tc.want)
		}
	}
	for _, host := range []string{"ftp://localhost:11434", "http:///", "http://user:password@localhost", "http://localhost/v1", "http://localhost?model=qwen", "http://localhost#fragment"} {
		if _, err := newOllamaConfig("qwen3.8:27b", host, 65536); err == nil {
			t.Errorf("accepted invalid Ollama host %q", host)
		}
	}
	if _, err := newOllamaConfig("qwen3.8:27b", "", 0); err == nil {
		t.Error("accepted a nonpositive context budget")
	}
}

func TestOllamaChildIsolation(t *testing.T) {
	for key, value := range map[string]string{
		"ANTHROPIC_API_KEY": "operator-key", "ANTHROPIC_AUTH_TOKEN": "operator-token",
		"ANTHROPIC_PROFILE": "operator-profile", "CLAUDE_CODE_USE_BEDROCK": "1",
		"CLAUDE_CODE_USE_VERTEX": "1", "CLAUDE_CODE_USE_FOUNDRY": "1",
		"CLAUDE_CODE_OAUTH_TOKEN": "operator-oauth", "CLAUDE_CODE_GIT_BASH_PATH": `C:\portable-git\bin\bash.exe`,
		"MAX_THINKING_TOKENS": "10000", "ANYTHING_TEST_SENTINEL": "preserved",
	} {
		t.Setenv(key, value)
	}
	config, err := newOllamaConfig("qwen3.8:27b", "", 65536)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("claude", cliArgs("system", config, "--input-format", "stream-json")...)
	config.configure(cmd)
	env := map[string]string{}
	for _, entry := range cmd.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
	}
	for key, want := range map[string]string{
		"ANTHROPIC_API_KEY": "ollama", "ANTHROPIC_BASE_URL": config.baseURL,
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "65536",
		"CLAUDE_CODE_GIT_BASH_PATH":                `C:\portable-git\bin\bash.exe`,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", "ANYTHING_TEST_SENTINEL": "preserved",
	} {
		if env[key] != want {
			t.Errorf("child %s = %q, want %q", key, env[key], want)
		}
	}
	for _, key := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_PROFILE", "CLAUDE_CODE_USE_BEDROCK",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_OAUTH_TOKEN", "MAX_THINKING_TOKENS"} {
		if env[key] != "" {
			t.Errorf("child retained inherited %s", key)
		}
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "operator-key" || os.Getenv("MAX_THINKING_TOKENS") != "10000" {
		t.Error("configuring a child changed the operator's environment")
	}
	modelIndex := slices.Index(cmd.Args, "--model")
	if modelIndex < 0 || modelIndex+1 >= len(cmd.Args) || cmd.Args[modelIndex+1] != config.model || !slices.Contains(cmd.Args, "--bare") {
		t.Errorf("local session does not use the actual local model in bare mode: %q", cmd.Args)
	}
	wantClaude := []string{"-p", "--output-format", "stream-json", "--include-partial-messages", "--verbose",
		"--model", "opus", "--effort", "low", "--system-prompt", "system",
		"--tools", "", "--strict-mcp-config", "--setting-sources", "", "--no-session-persistence"}
	if got := cliArgs("system", nil); !slices.Equal(got, wantClaude) {
		t.Errorf("default Claude invocation changed: %q", got)
	}
}

func TestOllamaChildIsolationCaseInsensitive(t *testing.T) {
	const bashPath = `C:\portable-git\bin\bash.exe`
	t.Setenv("Claude_Code_Git_Bash_Path", bashPath)
	t.Setenv("Claude_Code_Use_Bedrock", "1")
	config, err := newOllamaConfig("qwen3.8:27b", "", 65536)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("claude")
	config.configure(cmd)
	env := map[string]string{}
	for _, entry := range cmd.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		env[strings.ToUpper(key)] = value
	}
	if env["CLAUDE_CODE_GIT_BASH_PATH"] != bashPath || env["CLAUDE_CODE_USE_BEDROCK"] != "" {
		t.Errorf("case-insensitive environment handling lost Git Bash or retained the provider override")
	}
}
