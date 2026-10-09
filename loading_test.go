package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const loadingTestHead = "<!doctype html><head><style>body{color:red}</style></head>"
const loadingTestBody = "<body><h1>Amber & Otter 🦦</h1>\n<script></script></body>"

func loadingTestBackend(t *testing.T, status int, complete bool) (apiBackend, *atomic.Int32, chan string) {
	t.Helper()
	previousAPI, previousSession := useAPI, sess
	useAPI, sess = true, nil
	t.Cleanup(func() { useAPI, sess = previousAPI, previousSession })
	calls, prompts := &atomic.Int32{}, make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var params struct {
			Messages []struct{ Content []struct{ Text string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			t.Errorf("decode model request: %v", err)
		} else if len(params.Messages) == 1 && len(params.Messages[0].Content) == 1 {
			prompts <- params.Messages[0].Content[0].Text
		}
		if status != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"generation unavailable"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		event := func(kind, data string) { fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data) }
		event("message_start", `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"local","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`)
		event("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		chunks := []string{loadingTestHead}
		if complete {
			chunks = append(chunks, loadingTestBody)
		}
		for _, text := range chunks {
			encoded, _ := json.Marshal(text)
			event("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, encoded))
		}
		if complete {
			event("content_block_stop", `{"type":"content_block_stop","index":0}`)
			event("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`)
			event("message_stop", `{"type":"message_stop"}`)
		}
	}))
	t.Cleanup(server.Close)
	config, err := newOllamaConfig("local", server.URL, 65536)
	if err != nil {
		t.Fatal(err)
	}
	return config.apiBackend(), calls, prompts
}

func loadingTestReplay(raw []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/order?view=compact", bytes.NewReader(raw))
	r.Header.Set(generationHeader, "1")
	r.Header.Set("Sec-Fetch-Dest", "empty")
	return r
}

func TestLoadingBrowserReplay(t *testing.T) {
	api, calls, prompts := loadingTestBackend(t, http.StatusOK, true)
	body := "artifact=" + strings.Repeat("x", (64<<10)-len("artifact="))
	r := httptest.NewRequest(http.MethodPost, "/order?view=compact", strings.NewReader(body))
	// Match the wire header present on a browser POST, not only Request.ContentLength.
	for key, value := range map[string]string{"Sec-Fetch-Dest": "document", "User-Agent": "test-browser", "Referer": "http://example.com/catalog", "Content-Type": "application/x-www-form-urlencoded", "Content-Length": "65536", "Cookie": "original-cookie-secret", "Authorization": "original-auth-secret"} {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	serve(r.Context(), api, w, r)
	if w.Code != http.StatusOK || calls.Load() != 0 || !strings.Contains(w.Body.String(), "Generating page…") {
		t.Fatalf("navigation did not return a loader without generation: status=%d calls=%d", w.Code, calls.Load())
	}
	_, encoded, ok := strings.Cut(w.Body.String(), "atob(")
	encoded, _, end := strings.Cut(encoded, ")")
	var raw []byte
	if !ok || !end || json.Unmarshal([]byte(encoded), &raw) != nil {
		t.Fatal("loader does not contain a decodable HTTP snapshot")
	}
	if bytes.Contains(raw, []byte("original-cookie-secret")) || bytes.Contains(raw, []byte("original-auth-secret")) {
		t.Fatal("loader snapshot exposes credentials")
	}
	r = loadingTestReplay(raw)
	r.Header.Set("Cookie", "returning=browser")
	r.Header.Set("Authorization", "Basic browser-auth")
	w = httptest.NewRecorder()
	serve(r.Context(), api, w, r)
	if w.Code != http.StatusOK || calls.Load() != 1 || w.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("replay: status=%d calls=%d content-type=%q", w.Code, calls.Load(), w.Header().Get("Content-Type"))
	}
	var html strings.Builder
	done := 0
	decoder := json.NewDecoder(w.Body)
	for {
		var event struct {
			HTML, Error string
			Done        bool
		}
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil || event.Error != "" {
			t.Fatalf("invalid generation record: %+v error=%v", event, err)
		}
		html.WriteString(event.HTML)
		if event.Done {
			done++
		}
	}
	if html.String() != loadingTestHead+loadingTestBody || done != 1 {
		t.Fatalf("incorrect HTML/completion: html=%q done=%d", html.String(), done)
	}
	prompt := <-prompts
	for _, want := range []string{"POST /order?view=compact HTTP/1.1", "Referer: http://example.com/catalog", "User-Agent: test-browser", "Content-Type: application/x-www-form-urlencoded", "Cookie: returning=browser", "Authorization: Basic browser-auth", "\r\n\r\n" + body + "\n</request>"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("replay lost original request field (length %d)", len(want))
		}
	}
	if strings.Contains(prompt, generationHeader) {
		t.Error("internal marker reached the model")
	}
}

func TestLoadingFailuresAndRawHTML(t *testing.T) {
	for _, tc := range []struct {
		name               string
		status             int
		complete, internal bool
		raw                string
		wantStatus         int
		wantCalls          int32
		want               string
	}{
		{"upstream HTTP failure", 400, false, true, "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n", 502, 1, "generation unavailable"},
		{"premature EOF after head", 200, false, true, "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n", 200, 1, `"error":"model stream ended before message_stop"`},
		{"invalid replay", 200, true, true, "invalid", 400, 0, "malformed HTTP request"},
		{"oversized form", 200, true, true, "POST / HTTP/1.1\r\nHost: example.com\r\nContent-Length: 65537\r\n\r\n" + strings.Repeat("x", 65537), 400, 0, "request body too large"},
		{"raw nonbrowser HTML", 200, true, false, "", 200, 1, loadingTestHead + loadingTestBody},
		{"raw browser Claude HTML", 200, true, false, "", 200, 1, loadingTestHead + loadingTestBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, calls, _ := loadingTestBackend(t, tc.status, tc.complete)
			r := loadingTestReplay([]byte(tc.raw))
			if !tc.internal {
				r = httptest.NewRequest(http.MethodGet, "/", nil)
			}
			if tc.name == "raw browser Claude HTML" {
				r.Header.Set("Sec-Fetch-Dest", "document")
				api.local = false
			}
			w := httptest.NewRecorder()
			serve(r.Context(), api, w, r)
			if w.Code != tc.wantStatus || calls.Load() != tc.wantCalls || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("status=%d calls=%d body=%q", w.Code, calls.Load(), w.Body.String())
			}
			if tc.name == "premature EOF after head" {
				if strings.Contains(w.Body.String(), `"done":true`) || !strings.Contains(w.Body.String(), `"html":`) {
					t.Error("failed stream lost HTML or reported completion")
				}
			}
			if !tc.internal && (w.Body.String() != tc.want || w.Header().Get("Content-Type") != "text/html; charset=utf-8") {
				t.Error("raw HTML protocol changed")
			}
		})
	}
}
