package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"errors"
	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// TestCarrierProbeSurvey is a MEASUREMENT HARNESS and not a test of the
// shipped tree: it answers "would differential template discovery locate
// this port's version carrier, provably?" over a list of real ports, and
// prints a census. It ships nothing and asserts nothing about dockhand's
// behaviour today.
//
// THE METHOD. For a candidate span holding literal L in a port whose
// evaluated version is V: write a probe value P over L, evaluate, and
// read V_P. Take the longest common prefix and suffix of V and V_P; the
// middles are what L and P became. If middle(V) == L and middle(P) == P,
// the version is PROVEN to be prefix + <that literal> + suffix, and the
// literal to write for a target T is T with those affixes removed.
//
// It is a proof and not a heuristic: the composition function is
// OBSERVED by substitution rather than guessed from the text. A carrier
// that transforms its literal — [string map], [string range], a split —
// fails the middle test and is refused, which is the honest answer,
// because writing a target into a truncated sha would be wrong.
//
// Run it deliberately:
//
//	DOCKHAND_CARRIER_SURVEY=/path/to/ports go test ./internal/cli/ \
//	  -run TestCarrierProbeSurvey -timeout 60m -v
func TestCarrierProbeSurvey(t *testing.T) {
	root := os.Getenv("DOCKHAND_CARRIER_SURVEY")
	if root == "" {
		t.Skip("set DOCKHAND_CARRIER_SURVEY=<ports tree> to run the survey")
	}
	list := os.Getenv("DOCKHAND_CARRIER_PORTS")
	if list == "" {
		t.Skip("set DOCKHAND_CARRIER_PORTS=<file of port names, one per line>")
	}
	names := strings.Fields(strings.ReplaceAll(readFile(t, list), "\n", " "))

	s := &Services{TreeRoot: root, Tools: testFinder(), Err: os.Stderr, Out: os.Stderr}
	ctx := context.Background()
	require.NoError(t, s.Acquire(ctx, app.Needs{Tree: true, Evaluator: true}))
	defer s.Close()
	ev, err := s.Eval()
	require.NoError(t, err)
	tr, err := s.Tree()
	require.NoError(t, err)

	var reachable, ambiguous, templateOnly, noDrive, transformed, noCandidate, evalErr int
	for _, name := range names {
		target, terr := tr.Resolve(name)
		if terr != nil {
			evalErr++
			continue
		}
		h := portHandle(target, ev, s)
		outcome, detail := probeOne(ctx, h, name)
		switch outcome {
		case "reachable":
			reachable++
		case "AMBIGUOUS":
			ambiguous++
		case "template-only":
			templateOnly++
		case "no-drive":
			noDrive++
		case "transformed":
			transformed++
		case "no-candidate":
			noCandidate++
		default:
			evalErr++
		}
		fmt.Fprintf(os.Stderr, "%-14s %-28s %s\n", outcome, name, detail)
	}
	fmt.Fprintf(os.Stderr, "\n== %d ports ==\n reachable     %4d\n AMBIGUOUS     %4d\n template-only %4d\n transformed   %4d\n no-drive      %4d\n no-candidate  %4d\n error         %4d\n",
		len(names), reachable, ambiguous, templateOnly, transformed, noDrive, noCandidate, evalErr)
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}

// probeOne runs the method for one port and says what it found.
func probeOne(ctx context.Context, h port.Handle, name string) (string, string) {
	src, cst, err := h.Source()
	if err != nil {
		return "error", err.Error()
	}
	vals, err := h.Values(ctx)
	if err != nil {
		return "error", err.Error()
	}
	if vals.Version == "" {
		return "error", "no evaluated version"
	}

	spans := candidateSpans(src, cst, vals)
	if len(spans) == 0 {
		return "no-candidate", "nothing collected"
	}
	// A SYNTHETIC TARGET, because this asks whether the METHOD could
	// bump the port and not what upstream happens to publish today: the
	// last numeric run of the evaluated version, incremented. That is
	// what an ordinary patch bump looks like, and it needs no network.
	want := bumpLast(vals.Version)
	if want == "" {
		return "error", "no numeric segment to bump"
	}
	var anyTemplate, transformed string
	var hits []string
	for _, sp := range spans {
		lit := sp.Text(src)
		probe := probeValueFor(lit)
		if probe == "" || probe == lit {
			continue
		}
		probed, err := edit.Apply(src, []edit.Edit{{
			Kind: edit.Version, Start: sp.Start, End: sp.End,
			Old: lit, New: probe, Reason: "carrier probe",
		}})
		if err != nil {
			continue
		}
		shadow, cleanup, err := h.Shadow(probed)
		if err != nil {
			continue
		}
		sv, verr := shadow.Values(ctx)
		cleanup()
		if verr != nil || sv.Version == "" || sv.Version == vals.Version {
			continue // this span does not drive the version
		}
		pre, suf := affixes(vals.Version, sv.Version)
		midV := vals.Version[len(pre) : len(vals.Version)-len(suf)]
		midP := sv.Version[len(pre) : len(sv.Version)-len(suf)]
		if midV != lit || midP != probe {
			transformed = fmt.Sprintf("%q -> %q writing %q over %q", vals.Version, sv.Version, probe, lit)
			continue // the literal is transformed; try another candidate
		}
		// A TEMPLATE IS NOT ENOUGH — IT MUST EXPRESS THE TARGET. helm's
		// "4.2.4" probes proven as "" + <4.2> + ".4", which is true and
		// useless: no edit to "4.2" reaches 4.2.5. The candidate to pick
		// is the one whose affixes bracket the target too.
		anyTemplate = fmt.Sprintf("%q = %q + <%s> + %q", vals.Version, pre, lit, suf)
		if strings.HasPrefix(want, pre) && strings.HasSuffix(want, suf) && len(want) >= len(pre)+len(suf) {
			hits = append(hits, fmt.Sprintf("%s -> write %q", anyTemplate, want[len(pre):len(want)-len(suf)]))
		}
	}
	switch {
	case len(hits) == 1:
		return "reachable", hits[0] + " for " + want
	case len(hits) > 1:
		return "AMBIGUOUS", fmt.Sprintf("%d candidates express %s: %s", len(hits), want, strings.Join(hits, " | "))
	case anyTemplate != "":
		return "template-only", anyTemplate + " cannot express " + want
	case transformed != "":
		return "transformed", transformed
	}
	return "no-drive", fmt.Sprintf("%d candidate(s), none moved the version", len(spans))
}

// candidateSpans is every span the method may probe: what the locator
// already collects, PLUS the `set` word the shipped decline list
// deliberately drops ("the counterfactual probe should not chase
// coincidental sets"). Dropping them is what makes a computed carrier
// unreachable, so a survey of whether they are reachable must include
// them.
//
// The corroborated span goes FIRST when there is one, so the census
// counts today's answer before any new one.
func candidateSpans(src []byte, cst *syntax.Script, vals info.Values) []text.Span {
	var out []text.Span
	if loc, err := portstyle.Locate(src, cst, vals, info.FieldVersion); err == nil {
		out = append(out, loc.Span)
	} else {
		var d *portstyle.Decline
		if errors.As(err, &d) {
			for _, c := range d.Candidates {
				out = append(out, c.Span)
			}
		}
	}
	for cmd := range cst.Commands(src, portstyle.ScopeOf(src, vals.Name)) {
		if name, ok := cmd.Name(src); ok && name == "set" && len(cmd.Words) > 2 {
			out = append(out, cmd.Words[2].Span)
		}
	}
	return out
}

// probeValueFor is a value of the same lexical shape as the literal, so
// the evaluation does not fail on a value it would refuse: digits stay
// digits, so an expr or a comparison still runs.
func probeValueFor(lit string) string {
	if lit == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range lit {
		switch {
		case r >= '0' && r <= '9':
			if r == '7' {
				b.WriteRune('4')
			} else {
				b.WriteRune('7')
			}
		case r >= 'a' && r <= 'z':
			if r == 'q' {
				b.WriteRune('w')
			} else {
				b.WriteRune('q')
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// affixes is the longest common prefix and suffix of two versions, which
// bracket where the literal landed.
func affixes(a, b string) (string, string) {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	j := 0
	for j < len(a)-i && j < len(b)-i && a[len(a)-1-j] == b[len(b)-1-j] {
		j++
	}
	return a[:i], a[len(a)-j:]
}

// bumpLast increments the last run of digits in a version, which is what
// an ordinary patch release does to one.
func bumpLast(v string) string {
	end := -1
	for i := len(v) - 1; i >= 0; i-- {
		if v[i] >= '0' && v[i] <= '9' {
			end = i + 1
			break
		}
	}
	if end < 0 {
		return ""
	}
	start := end
	for start > 0 && v[start-1] >= '0' && v[start-1] <= '9' {
		start--
	}
	n, err := strconv.Atoi(v[start:end])
	if err != nil {
		return ""
	}
	return v[:start] + strconv.Itoa(n+1) + v[end:]
}
