package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
)

var logo = []string{
	` █████╗ ███╗   ██╗██╗   ██╗████████╗██╗  ██╗██╗███╗   ██╗ ██████╗ `,
	`██╔══██╗████╗  ██║╚██╗ ██╔╝╚══██╔══╝██║  ██║██║████╗  ██║██╔════╝ `,
	`███████║██╔██╗ ██║ ╚████╔╝    ██║   ███████║██║██╔██╗ ██║██║  ███╗`,
	`██╔══██║██║╚██╗██║  ╚██╔╝     ██║   ██╔══██║██║██║╚██╗██║██║   ██║`,
	`██║  ██║██║ ╚████║   ██║      ██║   ██║  ██║██║██║ ╚████║╚██████╔╝`,
	`╚═╝  ╚═╝╚═╝  ╚═══╝   ╚═╝      ╚═╝   ╚═╝  ╚═╝╚═╝╚═╝  ╚═══╝ ╚═════╝ `,
}

// One per start, like everything else here.
var taglines = []string{
	"nothing here exists until you ask for it",
	"every page is the first and last of its kind",
	"refresh to destroy",
	"there is no 404. there is only more.",
	"the server is dreaming in HTML",
	"no files. no database. no problem.",
	"you can't go back. the back button lies.",
	"all links lead somewhere. none of them existed a second ago.",
	"screenshots are the only archive",
	"serving pages from the void since just now",
}

// 256-colour gradient, top row to bottom: magenta through violet to cyan.
var gradient = []int{201, 165, 129, 93, 63, 51}

func printBanner(addr string, rows [][2]string) {
	color := useColor()
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}

	var b strings.Builder
	b.WriteString("\n")
	for i, line := range logo {
		b.WriteString("  " + paint(fmt.Sprintf("1;38;5;%d", gradient[i]), line) + "\n")
	}
	tag := taglines[rand.IntN(len(taglines))]
	b.WriteString("\n  " + paint("2", "░▒▓ ") + paint("3;38;5;219", tag) + paint("2", " ▓▒░") + "\n\n")

	row := func(k, v string) { b.WriteString("  " + paint("2", fmt.Sprintf("%-8s", k)) + v + "\n") }
	row("url", paint("1;4;38;5;51", "http://"+addr+"/")+paint("2", "  ← try any path"))
	for _, r := range rows {
		row(r[0], r[1])
	}
	b.WriteString("\n")
	fmt.Fprint(os.Stderr, b.String())
}

// useColor reports whether stderr is a terminal and NO_COLOR is unset.
func useColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
