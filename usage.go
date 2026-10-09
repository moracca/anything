package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// usageGroups orders the flags for -h, with a placeholder for each value.
// Descriptions come from the flag definitions themselves.
var usageGroups = []struct {
	title string
	flags [][2]string // flag name, value placeholder
}{
	{"Mode", [][2]string{{"session", ""}, {"recycle", "N"}}},
	{"Backend", [][2]string{{"ollama", "MODEL"}, {"ollama-context", "N"}}},
	{"Site", [][2]string{{"theme", "TEXT"}}},
	{"Seeds", [][2]string{{"seeds", "a,b,c"}, {"seed-file", "PATH"}, {"seed-mix", "a,b"}, {"no-seeds", ""}}},
}

var usageEnv = [][2]string{
	{"PORT", "port to listen on, on 127.0.0.1 (default 8080)"},
	{"ANTHROPIC_API_KEY", "if set (and -session and -ollama are off), generate pages through the Anthropic API instead of the claude CLI"},
	{"OLLAMA_HOST", "Ollama API base URL, as host:port or http(s)://host:port (default http://127.0.0.1:11434)"},
}

const (
	usageWidth  = 80 // fits any terminal
	usageIndent = 2
	usageColumn = 22 // where descriptions start
)

func printUsage(w io.Writer) {
	fmt.Fprint(w, "anything: a web server that invents every page\n\nUsage: anything [flags]\n")
	for _, g := range usageGroups {
		fmt.Fprintf(w, "\n%s:\n", g.title)
		for _, f := range g.flags {
			fl := flag.Lookup(f[0])
			if fl == nil {
				continue
			}
			name := "-" + fl.Name
			if f[1] != "" {
				name += " " + f[1]
			}
			usageRow(w, name, fl.Usage)
		}
	}
	// Any flag not placed in a group above still gets listed.
	listed := map[string]bool{}
	for _, g := range usageGroups {
		for _, f := range g.flags {
			listed[f[0]] = true
		}
	}
	var other []*flag.Flag
	flag.VisitAll(func(f *flag.Flag) {
		if !listed[f.Name] {
			other = append(other, f)
		}
	})
	if len(other) > 0 {
		fmt.Fprint(w, "\nOther:\n")
		for _, f := range other {
			name, usage := flag.UnquoteUsage(f)
			if name != "" {
				name = " " + name
			}
			usageRow(w, "-"+f.Name+name, usage)
		}
	}
	fmt.Fprint(w, "\nEnvironment:\n")
	for _, e := range usageEnv {
		usageRow(w, e[0], e[1])
	}
	fmt.Fprint(w, "\nWhile it's running, type h and press Enter for live commands.\n")
}

// usageRow prints a name and its description, wrapping the description in
// its own column so every name stays visible at the left edge.
func usageRow(w io.Writer, name, desc string) {
	pad := strings.Repeat(" ", usageColumn)
	lines := wrap(desc, usageWidth-usageColumn)
	head := strings.Repeat(" ", usageIndent) + name
	if len(head) < usageColumn-1 {
		fmt.Fprintf(w, "%-*s%s\n", usageColumn, head, lines[0])
		lines = lines[1:]
	} else {
		fmt.Fprintln(w, head) // too long to share a line: description goes below
	}
	for _, l := range lines {
		fmt.Fprintln(w, pad+l)
	}
}

// wrap breaks s into lines of at most width characters, at spaces.
func wrap(s string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	return append(lines, line)
}
