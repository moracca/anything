package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// builtinSeeds is the default pool when no -seeds or -seed-file is given.
var builtinSeeds = strings.Fields(`lighthouse moth archive basement orbit velvet ledger tide
	rust carnival quarantine orchard telegraph glacier casino monastery
	subway fungus ballroom observatory swamp vending-machine cathedral
	pawnshop aquarium hangar laundromat bunker greenhouse`)

// seedSource picks the seed offered to the model with each request. The
// randomness is the program's: asked for "random" words, a model gives its
// favourite ones, run after run.
type seedSource struct {
	pool []string // one is picked per request
	mix  []string // if set, one of these is paired with each pick from pool
	desc string   // for the banner
}

// pick returns this request's seed, or "" when seeds are off.
func (s seedSource) pick() string {
	if len(s.pool) == 0 {
		return ""
	}
	w := s.pool[rand.IntN(len(s.pool))]
	if len(s.mix) > 0 {
		return s.mix[rand.IntN(len(s.mix))] + " + " + w
	}
	return w
}

// splitList splits a comma-separated flag value, dropping blanks.
func splitList(v string) []string {
	var out []string
	for _, w := range strings.Split(v, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

var wordRE = regexp.MustCompile(`\p{L}[\p{L}'’-]*\p{L}`)

// loadSeedFile pulls every distinct word of four letters or more out of any
// text file: a word list, a novel, lyrics, a manual. Each distinct word is
// equally likely, however often it appears, and the length floor drops most
// of "the", "and", "was".
func loadSeedFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var words []string
	for _, w := range wordRE.FindAllString(string(data), -1) {
		key := strings.ToLower(w)
		if utf8.RuneCountInString(w) < 4 || seen[key] {
			continue
		}
		seen[key] = true
		words = append(words, w)
	}
	if len(words) == 0 {
		return nil, fmt.Errorf("%s: no words of four letters or more", path)
	}
	return words, nil
}
