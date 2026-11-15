package main

import (
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// unperturbedClosedReads uses the second observation's host-access events,
// not its ledger: only answers still marked closed were not substituted.
// Each recorded call contributes at most once to a context's tally.
func unperturbedClosedReads(p macports.PortObservation) []string {
	calls := map[string]bool{}
	for _, d := range p.Declarations {
		if d.Command != "dockhand.host-access" || len(d.Values) < 4 || d.Values[1] != "closed" || d.Values[3] == "" {
			continue
		}
		call := d.Values[3]
		words, errs := syntax.ListValues(call)
		if len(errs) == 0 && len(words) == 2 && strings.TrimPrefix(words[0], "::") == "sysctl" {
			call = "sysctl " + syntax.Quote(words[1])
		}
		calls[call] = true
	}
	var out []string
	for call := range calls {
		out = append(out, call)
	}
	sort.Strings(out)
	return out
}
