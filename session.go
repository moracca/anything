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
)

// session is one long-running `claude -p --input-format stream-json` process.
// Each HTTP request becomes a user turn, so the model remembers every page it
// has served. Turns are strictly one at a time; concurrent requests queue on mu.
type session struct {
	recycleAfter int

	mu    sync.Mutex
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Scanner
	pages int
}

func (s *session) start() error {
	cmd := exec.Command("claude", cliArgs(systemPrompt+sessionPrompt, "--input-format", "stream-json")...)
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
	s.cmd, s.stdin, s.out, s.pages = cmd, stdin, sc, 0
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
func (s *session) generate(prompt string, emit func(string)) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.recycleAfter > 0 && s.pages >= s.recycleAfter {
		log.Printf("session: %d pages served; forgetting everything", s.pages)
		s.stop()
	}
	if s.cmd == nil {
		if err := s.start(); err != nil {
			return "", err
		}
	}

	msg, _ := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": prompt},
	})
	if _, err := s.stdin.Write(append(msg, '\n')); err != nil {
		s.stop()
		return "", fmt.Errorf("session: write: %w", err)
	}

	var stopReason string
	for s.out.Scan() {
		var l cliLine
		if json.Unmarshal(s.out.Bytes(), &l) != nil {
			continue
		}
		switch {
		case l.Type == "stream_event" && l.Event.Type == "content_block_delta" && l.Event.Delta.Type == "text_delta":
			emit(l.Event.Delta.Text)
		case l.Type == "stream_event" && l.Event.Type == "message_delta":
			stopReason = l.Event.Delta.StopReason
		case l.Type == "result":
			s.pages++
			if l.IsError {
				return stopReason, fmt.Errorf("session: %s", l.Result)
			}
			return stopReason, nil
		}
	}
	// stdout closed before the turn finished: the process died. Restart next time.
	err := s.out.Err()
	s.stop()
	return stopReason, fmt.Errorf("session: claude exited mid-turn (%v); memory lost, will restart", err)
}
