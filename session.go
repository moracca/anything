package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// session is one long-running `claude -p --input-format stream-json` process.
// Each HTTP request becomes a user turn, so the model remembers every page it
// has served. Turns are strictly one at a time; concurrent requests queue on mu.
type session struct {
	recycleAfter int
	ollama       *ollamaConfig

	mu    sync.Mutex
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Scanner
	pages atomic.Int64 // pages in the current session's memory
	theme string       // the theme this session last heard about

	// The CLI reports cost and API time as running totals for the session;
	// these hold the previous totals so each page can be logged on its own.
	costSoFar float64
	apiSoFar  time.Duration
	// The CLI reports subscription usage only when it changes; keep the last report.
	lastPlan string
}

func (s *session) start() error {
	s.theme = currentTheme()
	cmd := exec.Command("claude", cliArgs(sitePrompt(true), s.ollama, "--input-format", "stream-json")...)
	if s.ollama != nil {
		s.ollama.configure(cmd)
	}
	cmd.Dir = os.TempDir()
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	s.cmd, s.stdin, s.out = cmd, stdin, sc
	s.pages.Store(0)
	s.costSoFar, s.apiSoFar = 0, 0
	log.Printf("session: started claude (pid %d); the site's memory begins now", cmd.Process.Pid)
	return nil
}

func (s *session) stop() {
	if s.cmd == nil {
		return
	}
	s.stdin.Close()
	s.cmd.Process.Kill()
	s.cmd.Wait()
	s.cmd = nil
}

// generate runs one turn. It can't be cancelled midway: if the visitor leaves,
// the page is still written (to nobody) and stays in the session's memory.
func (s *session) generate(prompt string, emit func(string)) (genStats, error) {
	stats := genStats{CostUSD: -1}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.recycleAfter > 0 && s.pages.Load() >= int64(s.recycleAfter) {
		log.Printf("session: %d pages served; forgetting everything", s.pages.Load())
		s.stop()
	}
	if s.cmd == nil {
		if err := s.start(); err != nil {
			return stats, err
		}
	}

	// A theme changed with the t command reaches a running session as a note
	// on its next page, so the site keeps its memory while its world shifts.
	if th := currentTheme(); th != s.theme {
		note := "Note from whoever runs this server: the site no longer has a theme; pages are free of it from now on."
		if th != "" {
			note = "Note from whoever runs this server: the site's theme is now: " + th +
				". From this page on, everything belongs to the new theme; what came before can echo, but the world has changed."
		}
		prompt = note + "\n\n" + prompt
		s.theme = th
	}

	msg, _ := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": prompt},
	})
	if _, err := s.stdin.Write(append(msg, '\n')); err != nil {
		s.stop()
		return stats, fmt.Errorf("session: write: %w", err)
	}

	for s.out.Scan() {
		var l cliLine
		if json.Unmarshal(s.out.Bytes(), &l) != nil {
			continue
		}
		if s.ollama != nil && l.Type == "rate_limit_event" {
			continue
		}
		if l.apply(&stats, emit) {
			s.pages.Add(1)
			stats.CostUSD -= s.costSoFar
			stats.APITime -= s.apiSoFar
			s.costSoFar, s.apiSoFar = l.TotalCostUSD, time.Duration(l.DurationAPIMs)*time.Millisecond
			if stats.Plan == "" {
				stats.Plan = s.lastPlan
			}
			s.lastPlan = stats.Plan
			if s.ollama != nil {
				stats.CostUSD = -1 // CLI prices do not describe local inference.
			}
			if l.IsError {
				return stats, fmt.Errorf("session: %s", l.Result)
			}
			return stats, nil
		}
	}
	// stdout closed before the turn finished: the process died. Restart next time.
	err := s.out.Err()
	s.stop()
	return stats, fmt.Errorf("session: claude exited mid-turn (%v); memory lost, will restart", err)
}

// reset ends the session, waiting for any page being made. The next request
// starts a new one with no memory. It returns how many pages were forgotten.
func (s *session) reset() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.pages.Load()
	s.stop()
	s.pages.Store(0)
	return n
}

// remembered is how many pages the current session holds in memory.
func (s *session) remembered() int64 { return s.pages.Load() }
