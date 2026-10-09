package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestServeOperatorControls(t *testing.T) {
	previousSeeds, previousTotals := seeds, totals
	t.Cleanup(func() { seeds, totals = previousSeeds, previousTotals; takeNote() })
	for _, tc := range []struct {
		name, seed string
		source     seedSource
	}{
		{"seed", "velvet", seedSource{pool: []string{"velvet"}}},
		{"mixed seed", "rain + velvet", seedSource{pool: []string{"velvet"}, mix: []string{"rain"}}},
		{"no seeds", "", seedSource{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, _, prompts := loadingTestBackend(t, http.StatusOK, true)
			seeds, totals = tc.source, &runTotals{started: time.Now()}
			addNote("the station has begun to flood")
			for i := range 2 {
				r := httptest.NewRequest(http.MethodGet, "/observatory", nil)
				w := httptest.NewRecorder()
				serve(r.Context(), api, w, r)
				if w.Code != http.StatusOK {
					t.Fatalf("generation failed: %d %s", w.Code, w.Body.String())
				}
				prompt := <-prompts
				if strings.Contains(prompt, "Seed for this visit") != (tc.seed != "") ||
					(tc.seed != "" && !strings.Contains(prompt, ": "+tc.seed+"\n\n")) {
					t.Errorf("incorrect seed in prompt: %q", prompt)
				}
				if strings.Contains(prompt, "the station has begun to flood") != (i == 0) {
					t.Errorf("operator note was not delivered exactly once: %q", prompt)
				}
			}
			if totals.pages != 2 || totals.outTokens != 4 || totals.errors != 0 || totals.costKnown {
				t.Errorf("incorrect local run totals: %+v", totals)
			}
		})
	}
}

func TestProgressLogsAreLocal(t *testing.T) {
	previousWriter := log.Writer()
	t.Cleanup(func() { log.SetOutput(previousWriter) })
	for _, local := range []bool{false, true} {
		api, _, _ := loadingTestBackend(t, http.StatusOK, true)
		api.local = local
		var output bytes.Buffer
		log.SetOutput(&output)
		r := httptest.NewRequest(http.MethodGet, "/observatory", nil)
		serve(r.Context(), api, httptest.NewRecorder(), r)
		for _, marker := range []string{"; generating HTML", "HTML started after"} {
			if strings.Contains(output.String(), marker) != local {
				t.Errorf("local=%v: unexpected progress log: %s", local, output.String())
			}
		}
		wantLines := 2
		if local {
			wantLines++
		}
		if strings.Count(output.String(), "\n") != wantLines {
			t.Errorf("local=%v: expected %d log lines: %s", local, wantLines, output.String())
		}
	}
}

type testSessionInput struct{ bytes.Buffer }

func (*testSessionInput) Close() error { return nil }

func TestLocalSessionThemeChange(t *testing.T) {
	previousTheme := currentTheme()
	t.Cleanup(func() { setTheme(previousTheme) })
	setTheme("deep sea research station")
	input := &testSessionInput{}
	s := &session{
		local: &localConfig{model: "local"}, theme: "old theme",
		cmd: &exec.Cmd{}, stdin: input,
		out: bufio.NewScanner(strings.NewReader(`{"type":"result","usage":{"output_tokens":2}}`)),
	}
	if _, err := s.generate("<request>GET /observatory</request>", func(string) {}); err != nil {
		t.Fatal(err)
	}
	var sent struct{ Message struct{ Content string } }
	if err := json.Unmarshal(input.Bytes(), &sent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent.Message.Content, "the site's theme is now: deep sea research station") ||
		!strings.HasSuffix(sent.Message.Content, "<request>GET /observatory</request>") || s.pages.Load() != 1 {
		t.Fatalf("local session lost the live theme update or turn: %q", sent.Message.Content)
	}
}
