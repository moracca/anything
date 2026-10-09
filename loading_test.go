package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const loadingTestHead = "<!doctype html><head><style>body{color:red}</style></head>"
const loadingTestBody = "<body><h1>Amber & Otter 🦦</h1>\n<script></script></body>"

func loadingTestBackend(t *testing.T, status int, complete bool) (apiBackend, *atomic.Int32, chan string) {
	t.Helper()
	previousAPI, previousSession := useAPI, sess
	previousSeeds, previousNote := seeds, takeNote()
	pendingPages.Lock()
	previousPages := pendingPages.requests
	pendingPages.requests = make(map[string]pendingPage)
	pendingPages.Unlock()
	useAPI, sess = true, nil
	seeds = seedSource{pool: []string{"velvet"}}
	t.Cleanup(func() {
		useAPI, sess, seeds = previousAPI, previousSession, previousSeeds
		takeNote()
		addNote(previousNote)
		pendingPages.Lock()
		pendingPages.requests = previousPages
		pendingPages.Unlock()
	})
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

func loadingTestReplay(token string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/order?view=compact", nil)
	r.Header.Set(generationHeader, token)
	r.Header.Set("Sec-Fetch-Dest", "empty")
	return r
}

func loadingTestLoader(t *testing.T, api apiBackend, r *http.Request) (string, *httptest.ResponseRecorder) {
	t.Helper()
	r.Header.Set("Sec-Fetch-Dest", "document")
	w := httptest.NewRecorder()
	serve(r.Context(), api, w, r)
	_, encoded, ok := strings.Cut(w.Body.String(), `headers:{"X-Anything-Generate":`)
	encoded, _, end := strings.Cut(encoded, "}")
	var token string
	if w.Code != http.StatusOK || !ok || !end || json.Unmarshal([]byte(encoded), &token) != nil || len(token) != 64 {
		t.Fatalf("invalid loader/token: status=%d body=%q", w.Code, w.Body.String())
	}
	return token, w
}

func TestLoadingBrowserReplay(t *testing.T) {
	api, calls, prompts := loadingTestBackend(t, http.StatusOK, true)
	addNote("Use amber lettering")
	body := "artifact=" + strings.Repeat("x", (64<<10)-len("artifact="))
	r := httptest.NewRequest(http.MethodPost, "/order?view=compact", strings.NewReader(body))
	// Match the wire header present on a browser POST, not only Request.ContentLength.
	for key, value := range map[string]string{"Sec-Fetch-Dest": "document", "User-Agent": "test-browser", "Referer": "http://example.com/catalog", "Content-Type": "application/x-www-form-urlencoded", "Content-Length": "65536", "Cookie": "original-cookie-secret", "Authorization": "original-auth-secret"} {
		r.Header.Set(key, value)
	}
	token, w := loadingTestLoader(t, api, r)
	if calls.Load() != 0 || peekNote() != "Use amber lettering" {
		t.Fatal("loader generated a page or consumed the operator note")
	}
	pendingPages.Lock()
	saved := string(pendingPages.requests[token].raw)
	pendingPages.Unlock()
	if strings.Contains(saved, "original-cookie-secret") || strings.Contains(saved, "original-auth-secret") {
		t.Fatal("saved request retains credentials")
	}
	for _, private := range []string{"original-cookie-secret", "original-auth-secret", body} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("loader exposes original request data")
		}
	}
	r = loadingTestReplay(token)
	// Supplying different HTTP bytes must not change the saved navigation.
	r.Body = io.NopCloser(strings.NewReader("GET /forged HTTP/1.1\r\nHost: forged\r\n\r\n"))
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
	for _, want := range []string{"Use amber lettering", "POST /order?view=compact HTTP/1.1", "Referer: http://example.com/catalog", "User-Agent: test-browser", "Content-Type: application/x-www-form-urlencoded", "Cookie: returning=browser", "Authorization: Basic browser-auth", "\r\n\r\n" + body + "\n</request>"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("replay lost original request field (length %d)", len(want))
		}
	}
	if strings.Contains(prompt, generationHeader) || strings.Contains(prompt, "/forged") || peekNote() != "" {
		t.Error("generation leaked replay data or failed to consume the note")
	}
	r = loadingTestReplay(token)
	w = httptest.NewRecorder()
	serve(r.Context(), api, w, r)
	if w.Code != http.StatusForbidden || calls.Load() != 1 {
		t.Error("reusing a token caused generation")
	}
}

func TestLoadingFailuresAndRawHTML(t *testing.T) {
	for _, tc := range []struct {
		name               string
		status             int
		complete, internal bool
		wantStatus         int
		wantCalls          int32
		want               string
	}{
		{"upstream HTTP failure", 400, false, true, 502, 1, "generation unavailable"},
		{"premature EOF after head", 200, false, true, 200, 1, `"error":"model stream ended before message_stop"`},
		{"oversized form", 200, true, false, 400, 0, "request body too large"},
		{"raw nonbrowser HTML", 200, true, false, 200, 1, loadingTestHead + loadingTestBody},
		{"raw browser Claude HTML", 200, true, false, 200, 1, loadingTestHead + loadingTestBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, calls, _ := loadingTestBackend(t, tc.status, tc.complete)
			r := httptest.NewRequest(http.MethodGet, "/order?view=compact", nil)
			if tc.internal {
				token, _ := loadingTestLoader(t, api, r)
				r = loadingTestReplay(token)
			}
			if tc.name == "oversized form" {
				r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 65537)))
				r.Header.Set("Sec-Fetch-Dest", "document")
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
			if strings.HasPrefix(tc.name, "raw ") && (w.Body.String() != tc.want || w.Header().Get("Content-Type") != "text/html; charset=utf-8") {
				t.Error("raw HTML protocol changed")
			}
		})
	}
}

func TestLoadingTokenAuthorization(t *testing.T) {
	for _, name := range []string{"unknown", "expired", "nonlocal", "wrong method", "ordinary fetch"} {
		t.Run(name, func(t *testing.T) {
			api, calls, _ := loadingTestBackend(t, 200, true)
			token, _ := loadingTestLoader(t, api, httptest.NewRequest(http.MethodGet, "/order?view=compact", nil))
			r := loadingTestReplay(token)
			wantStatus := http.StatusForbidden
			switch name {
			case "unknown":
				r.Header.Set(generationHeader, "1")
			case "expired":
				pendingPages.Lock()
				page := pendingPages.requests[token]
				page.expires = time.Now().Add(-time.Second)
				pendingPages.requests[token] = page
				pendingPages.Unlock()
			case "nonlocal":
				api.local = false
			case "wrong method":
				r.Method = http.MethodGet
			case "ordinary fetch":
				r.Header.Del(generationHeader)
				wantStatus = http.StatusNotFound
			}
			w := httptest.NewRecorder()
			serve(r.Context(), api, w, r)
			if w.Code != wantStatus || calls.Load() != 0 {
				t.Fatalf("status=%d calls=%d", w.Code, calls.Load())
			}
		})
	}
}

func TestLoadingTokenConcurrentConsumption(t *testing.T) {
	api, calls, _ := loadingTestBackend(t, 200, true)
	token, _ := loadingTestLoader(t, api, httptest.NewRequest(http.MethodGet, "/order?view=compact", nil))
	var workers sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		workers.Go(func() {
			r, w := loadingTestReplay(token), httptest.NewRecorder()
			serve(r.Context(), api, w, r)
			statuses <- w.Code
		})
	}
	workers.Wait()
	first, second := <-statuses, <-statuses
	if calls.Load() != 1 || !((first == 200 && second == 403) || (first == 403 && second == 200)) {
		t.Fatalf("token generated %d times; statuses=%d,%d", calls.Load(), first, second)
	}
}

func TestLoadingTokenPruningAndLimit(t *testing.T) {
	loadingTestBackend(t, 200, true)
	pendingPages.Lock()
	for i := range generationTokenLimit {
		pendingPages.requests[fmt.Sprint(i)] = pendingPage{expires: time.Now().Add(-time.Second)}
	}
	pendingPages.Unlock()
	token, err := storePendingPage([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	if err != nil {
		t.Fatalf("expired entries prevented issuing a token: %v", err)
	}
	pendingPages.Lock()
	if len(pendingPages.requests) != 1 {
		t.Error("mint did not prune expired entries")
	}
	for i := range generationTokenLimit - 1 {
		pendingPages.requests[fmt.Sprint(i)] = pendingPage{expires: time.Now().Add(time.Minute)}
	}
	pendingPages.Unlock()
	if _, err := storePendingPage(nil); err == nil {
		t.Error("pending request limit was not enforced")
	}
	pendingPages.Lock()
	page := pendingPages.requests[token]
	page.expires = time.Now().Add(-time.Second)
	pendingPages.requests[token] = page
	pendingPages.Unlock()
	if _, err := originalRequest(loadingTestReplay(token)); err == nil {
		t.Error("expired token was accepted")
	}
	pendingPages.Lock()
	_, remains := pendingPages.requests[token]
	pendingPages.Unlock()
	if remains {
		t.Error("consume did not prune expired token")
	}
}
