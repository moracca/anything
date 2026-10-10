package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// genStats describes one page generation, for the log.
type genStats struct {
	StopReason   string
	Model        string
	InputTokens  int64 // uncached input
	CacheRead    int64
	CacheWrite   int64
	OutputTokens int64
	CostUSD      float64 // API price of the page; -1 if unknown
	Billed       bool    // true when CostUSD is actually charged (API key), not just an API-rate estimate
	APITime      time.Duration
	Plan         string // subscription usage, from the claude CLI
}

// Per-million-token API prices, for working out cost when the API doesn't
// report it (the claude CLI reports its own).
var prices = map[string]struct{ in, out, cacheRead, cacheWrite float64 }{
	"claude-opus-5-5":   {4.00, 20.00, 0.20, 5.00},
	"claude-sonnet-5-5": {2.00, 10.00, 0.20, 2.50},
	// Haiku 5.5's input/output prices are for prompts up to 100K tokens (pages are
	// far smaller); its cache prices are assumed at the usual 0.1x / 1.25x of input.
	"claude-haiku-5-5": {0.10, 0.50, 0.01, 0.125},
}

func apiCost(s genStats) float64 {
	p, ok := prices[s.Model]
	if !ok {
		return -1
	}
	return (float64(s.InputTokens)*p.in + float64(s.OutputTokens)*p.out +
		float64(s.CacheRead)*p.cacheRead + float64(s.CacheWrite)*p.cacheWrite) / 1e6
}

// context is everything the model read for this page. In -session mode it
// grows with every page: it's the size of the site's memory.
func (s genStats) context() int64 { return s.InputTokens + s.CacheRead + s.CacheWrite }

func (s genStats) String() string {
	if s.Model == "" && s.OutputTokens == 0 {
		return ""
	}
	parts := []string{strings.TrimPrefix(s.Model, "claude-")}
	parts = append(parts, fmt.Sprintf("context %s (%s cached)  out %s",
		kilo(s.context()), kilo(s.CacheRead), kilo(s.OutputTokens)))
	if s.OutputTokens > 0 && s.APITime > 0 {
		parts = append(parts, fmt.Sprintf("%.0f tok/s", float64(s.OutputTokens)/s.APITime.Seconds()))
	}
	switch {
	case s.CostUSD >= 0 && s.Billed:
		parts = append(parts, fmt.Sprintf("$%.3f", s.CostUSD))
	case s.CostUSD >= 0:
		parts = append(parts, fmt.Sprintf("≈$%.3f at API rates", s.CostUSD))
	}
	if s.Plan != "" {
		parts = append(parts, s.Plan)
	}
	if s.StopReason != "" && s.StopReason != "end_turn" {
		parts = append(parts, "stopped: "+s.StopReason)
	}
	return strings.Join(parts, " · ")
}

func kilo(n int64) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func seconds(d time.Duration) string { return fmt.Sprintf("%.1fs", d.Seconds()) }

// rateLimitInfo is the CLI's report of where a subscription stands against
// its usage limits.
type rateLimitInfo struct {
	Status         string `json:"status"`
	RateLimitType  string `json:"rateLimitType"`
	ResetsAt       int64  `json:"resetsAt"`
	UnifiedWindows map[string]struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    int64   `json:"resetsAt"`
	} `json:"unifiedWindows"`
}

var windowNames = map[string]string{"five_hour": "5h", "seven_day": "7d"}

// plan formats usage as e.g. "plan 5h 12% · 7d 40%", and reports whether
// it's worth a warning: limited, or a window at 80% or more.
func (ri rateLimitInfo) plan() (string, bool) {
	if ri.Status != "" && ri.Status != "allowed" {
		return fmt.Sprintf("plan LIMITED (%s) until %s", ri.RateLimitType,
			time.Unix(ri.ResetsAt, 0).Format("Jan 2 15:04")), true
	}
	keys := make([]string, 0, len(ri.UnifiedWindows))
	for k := range ri.UnifiedWindows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	warn := false
	for _, k := range keys {
		w := ri.UnifiedWindows[k]
		name := windowNames[k]
		if name == "" {
			name = k
		}
		part := fmt.Sprintf("%s %.0f%%", name, w.Utilization*100)
		if w.Utilization >= 0.8 {
			part += " (resets " + time.Unix(w.ResetsAt, 0).Format("Jan 2 15:04") + ")"
			warn = true
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", false
	}
	return "plan " + strings.Join(parts, " · "), warn
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
