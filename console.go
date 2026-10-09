package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Commands typed into the server's terminal. Only whoever runs the server
// can use them; nothing on the website can.
const consoleHelp = `commands (type, then Enter):
  r          reset the -session: the site forgets everything
  s          summary of pages, time, tokens and usage so far
  t          show the theme
  t <text>   change the theme for every page from now on
  t off      remove the theme
  n <text>   a note for the next page only; in -session mode it lives on in memory
  n          show the pending note
  n off      cancel it
  h          this help`

// The theme can change while pages are being made, so it's read through these.
var (
	themeMu sync.RWMutex
	theme   string
)

func currentTheme() string {
	themeMu.RLock()
	defer themeMu.RUnlock()
	return theme
}

func setTheme(t string) {
	themeMu.Lock()
	theme = t
	themeMu.Unlock()
}

// The note for the next page, from the n command. Several n commands before
// the next page add up; the page takes them all.
var (
	noteMu sync.Mutex
	note   string
)

func addNote(n string) {
	noteMu.Lock()
	defer noteMu.Unlock()
	if note != "" {
		note += " Also: "
	}
	note += n
}

func peekNote() string {
	noteMu.Lock()
	defer noteMu.Unlock()
	return note
}

// takeNote returns the pending note and clears it.
func takeNote() string {
	noteMu.Lock()
	defer noteMu.Unlock()
	n := note
	note = ""
	return n
}

// console reads commands from stdin until it closes (e.g. when stdin isn't a
// terminal), then quietly stops.
func console() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		cmd, arg, _ := strings.Cut(line, " ")
		arg = strings.TrimSpace(arg)
		switch strings.ToLower(cmd) {
		case "":
		case "r":
			if sess == nil {
				say("no session to reset: without -session every request already starts fresh")
				continue
			}
			say("resetting the session (after the page being made, if any)...")
			go func() {
				n := sess.reset()
				say(fmt.Sprintf("session reset: %d pages forgotten; the next page starts a new site", n))
			}()
		case "s":
			say(totals.summary())
		case "t":
			switch {
			case arg == "" && currentTheme() == "":
				say("no theme. set one with: t <text>")
			case arg == "":
				say("theme: " + currentTheme())
			case strings.EqualFold(arg, "off"):
				setTheme("")
				say("theme removed")
			default:
				setTheme(arg)
				say("theme is now: " + arg)
			}
		case "n":
			switch {
			case arg == "" && peekNote() == "":
				say("no note pending. add one with: n <text>")
			case arg == "":
				say("note for the next page: " + peekNote())
			case strings.EqualFold(arg, "off"):
				takeNote()
				say("note cancelled")
			default:
				addNote(arg)
				say("note for the next page: " + peekNote())
			}
		case "h", "help", "?":
			say(consoleHelp)
		default:
			say(fmt.Sprintf("unknown command %q\n%s", line, consoleHelp))
		}
	}
}

func say(s string) {
	for _, l := range strings.Split(s, "\n") {
		fmt.Fprintln(os.Stderr, "» "+l)
	}
}

// totals accumulates every page served, for the s command.
var totals = &runTotals{started: time.Now()}

type runTotals struct {
	mu                   sync.Mutex
	started              time.Time
	pages, left, errors  int
	skipped              int
	outTokens            int64
	cost                 float64
	billed, costKnown    bool
	firstByteSum, allSum time.Duration
	firstByteN           int
	lastContext          int64
	lastPlan             string
}

func (t *runTotals) addPage(s genStats, firstByte, total time.Duration, left, failed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pages++
	if left {
		t.left++
	}
	if failed {
		t.errors++
	}
	t.outTokens += s.OutputTokens
	if s.CostUSD >= 0 {
		t.cost += s.CostUSD
		t.costKnown = true
		t.billed = s.Billed
	}
	if firstByte > 0 {
		t.firstByteSum += firstByte
		t.firstByteN++
	}
	t.allSum += total
	if c := s.context(); c > 0 {
		t.lastContext = c
	}
	if s.Plan != "" {
		t.lastPlan = s.Plan
	}
}

func (t *runTotals) addSkip() {
	t.mu.Lock()
	t.skipped++
	t.mu.Unlock()
}

func (t *runTotals) summary() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "up %s · %d pages", time.Since(t.started).Round(time.Second), t.pages)
	if t.left > 0 {
		fmt.Fprintf(&b, " (%d unfinished when the visitor left)", t.left)
	}
	if t.errors > 0 {
		fmt.Fprintf(&b, " · %d failed", t.errors)
	}
	fmt.Fprintf(&b, " · %d requests skipped\n", t.skipped)
	if t.pages > 0 {
		fb := "-"
		if t.firstByteN > 0 {
			fb = seconds(t.firstByteSum / time.Duration(t.firstByteN))
		}
		fmt.Fprintf(&b, "average: first byte %s, total %s · %s output tokens in all\n",
			fb, seconds(t.allSum/time.Duration(t.pages)), kilo(t.outTokens))
	}
	if t.costKnown {
		if t.billed {
			fmt.Fprintf(&b, "cost: $%.3f billed\n", t.cost)
		} else {
			fmt.Fprintf(&b, "cost: ≈$%.3f at API rates (not billed on a subscription)\n", t.cost)
		}
	}
	if t.lastPlan != "" {
		b.WriteString(t.lastPlan + "\n")
	}
	if sess != nil {
		if n := sess.remembered(); n > 0 {
			fmt.Fprintf(&b, "session: %d pages remembered, memory %s tokens\n", n, kilo(t.lastContext))
		} else {
			b.WriteString("session: memory empty; the next page starts a new site\n")
		}
	}
	fmt.Fprintf(&b, "seeds: %s", seeds.desc)
	if th := currentTheme(); th != "" {
		fmt.Fprintf(&b, " · theme: %s", th)
	}
	return b.String()
}
