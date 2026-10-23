package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// compared are the commands a version bump changes and the replay checks:
// the version, the checksums, and the dependency blocks. Each is read as
// the multiset of its words, so a block's rows in another order are the
// same block, as go2port and cargo2port may order them otherwise than the
// person did.
var compared = map[string]bool{
	"version": true, "revision": true, "epoch": true,
	"go.setup": true, "github.setup": true, "gitlab.setup": true, "codeberg.setup": true,
	"checksums": true, "go.vendors": true,
	"cargo.crates": true, "cargo.crates_github": true, "cargo.crates_gitlab": true, "cargo.crates_git": true,
}

// statement is one compared command's words, sorted, keyed by its name;
// a command written twice keeps both, as a second checksums would be.
type statements map[string][]string

// read is a Portfile's compared commands at its top level and in its
// subports and variants, each command's words after its name sorted.
func read(src []byte) (statements, error) {
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return nil, fmt.Errorf("the Portfile doesn't parse: %v", errs[0])
	}
	out := statements{}
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		if len(cmd.Words) == 0 {
			continue
		}
		name, ok := cmd.Words[0].Literal(src)
		if !ok || !compared[name] {
			continue
		}
		var words []string
		for _, word := range cmd.Words[1:] {
			value, literal := word.Literal(src)
			if !literal {
				value = word.Span.Text(src)
			}
			words = append(words, strings.Fields(value)...)
		}
		if name == "checksums" {
			words = sums(words)
		}
		slices.Sort(words)
		out[name] = append(out[name], strings.Join(words, " "))
	}
	for name := range out {
		slices.Sort(out[name])
	}
	return out, nil
}

// sums are a checksums command's sums and sizes, each with its kind, its
// files' names apart: a bump may name a file ${distname}${extract.suffix}
// where the replay writes the name the version gives, the same file.
func sums(words []string) []string {
	var out []string
	for i := 0; i+1 < len(words); i++ {
		switch words[i] {
		case "md5", "sha1", "rmd160", "sha256", "size":
			out = append(out, words[i]+"="+words[i+1])
			i++
		}
	}
	return out
}

// differences are what got says that want doesn't, and the reverse, a
// line each, for each compared command the two Portfiles write
// otherwise; none where they agree.
func differences(want, got statements) []string {
	var found []string
	names := slices.Sorted(maps.Keys(compared))
	for _, name := range names {
		if slices.Equal(want[name], got[name]) {
			continue
		}
		missing, extra := wordsApart(strings.Fields(strings.Join(want[name], " ")), strings.Fields(strings.Join(got[name], " ")))
		found = append(found, fmt.Sprintf("%s: %s", name, apartWords(missing, extra)))
	}
	return found
}

// wordsApart are the words want has more of than got, and got of want.
func wordsApart(want, got []string) (missing, extra []string) {
	count := map[string]int{}
	for _, word := range want {
		count[word]++
	}
	for _, word := range got {
		count[word]--
	}
	for _, word := range slices.Sorted(maps.Keys(count)) {
		for n := count[word]; n > 0; n-- {
			missing = append(missing, word)
		}
		for n := count[word]; n < 0; n++ {
			extra = append(extra, word)
		}
	}
	return missing, extra
}

// apartWords says what's missing and extra, the first few of each.
func apartWords(missing, extra []string) string {
	few := func(words []string) string {
		if len(words) > 4 {
			return strings.Join(words[:4], " ") + fmt.Sprintf(" (and %d more)", len(words)-4)
		}
		return strings.Join(words, " ")
	}
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "the bump has "+few(missing))
	}
	if len(extra) > 0 {
		parts = append(parts, "the replay has "+few(extra))
	}
	if len(parts) == 0 {
		return "the same words, written as other commands"
	}
	return strings.Join(parts, "; ")
}
