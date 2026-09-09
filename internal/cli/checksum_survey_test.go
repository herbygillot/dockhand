package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// TestChecksumRefusalSurvey measures what the two proposed checksum
// refusals would cost: how many ports that plan today would begin to
// decline. It ships nothing.
//
// RULE T (text). Count the `checksums` commands the parser sees in this
// context, including the ones in branches this evaluation did not take.
// A command whose values appear NOWHERE in the evaluated checksums is a
// command no edit can land in — so a bump rewrites the taken branch and
// leaves it stale. This is the gh shape, and it is invisible in the
// predicted delta because the untaken branch never evaluates.
//
// RULE D (delta). The evaluated checksums name more distfiles than the
// evaluation will fetch. This is the terraform shape: one command,
// several file groups, and only the host's architecture retrieved.
//
// Both are computed from an evaluation alone — no network, no fetch —
// which is what makes surveying the whole candidate population cheap.
//
//	DOCKHAND_CHECKSUM_SURVEY=/path/to/ports DOCKHAND_CHECKSUM_PORTS=<file> \
//	  go test ./internal/cli/ -run TestChecksumRefusalSurvey -timeout 60m -v
func TestChecksumRefusalSurvey(t *testing.T) {
	root := os.Getenv("DOCKHAND_CHECKSUM_SURVEY")
	list := os.Getenv("DOCKHAND_CHECKSUM_PORTS")
	if root == "" || list == "" {
		t.Skip("set DOCKHAND_CHECKSUM_SURVEY and DOCKHAND_CHECKSUM_PORTS to run")
	}
	b, err := os.ReadFile(list)
	require.NoError(t, err)
	names := strings.Fields(string(b))

	s := &Services{TreeRoot: root, Tools: testFinder(), Err: os.Stderr, Out: os.Stderr}
	ctx := context.Background()
	require.NoError(t, s.Acquire(ctx, app.Needs{Tree: true, Evaluator: true}))
	defer s.Close()
	ev, err := s.Eval()
	require.NoError(t, err)
	tr, err := s.Tree()
	require.NoError(t, err)

	var clean, ruleT, ruleD, both, evalErr int
	var tConditional int
	for _, name := range names {
		target, terr := tr.Resolve(name)
		if terr != nil {
			evalErr++
			continue
		}
		h := portHandle(target, ev, s)
		t1, cond, d1, ok := surveyOne(ctx, h)
		if !ok {
			evalErr++
			continue
		}
		switch {
		case t1 && d1:
			both++
		case t1:
			ruleT++
			if cond {
				tConditional++
			}
		case d1:
			ruleD++
		default:
			clean++
		}
		if t1 || d1 {
			fmt.Fprintf(os.Stderr, "%-30s T=%-5v (conditional=%v) D=%v\n", name, t1, cond, d1)
		}
	}
	fmt.Fprintf(os.Stderr, "\n== %d ports ==\n clean            %5d\n rule T only      %5d  (of which conditional %d)\n rule D only      %5d\n both             %5d\n error            %5d\n",
		len(names), clean, ruleT, tConditional, ruleD, both, evalErr)
}

// surveyOne reports whether each rule would fire for one port, and
// whether rule T's offending block sits inside a conditional.
func surveyOne(ctx context.Context, h port.Handle) (ruleT, conditional, ruleD, ok bool) {
	src, cst, err := h.Source()
	if err != nil {
		return false, false, false, false
	}
	vals, err := h.Values(ctx)
	if err != nil {
		return false, false, false, false
	}
	evaluated := strings.Join(vals.Checksums, " ")

	// RULE T: a checksums command whose literals appear nowhere in the
	// evaluated checksums is one this evaluation never took.
	for cmd := range cst.Commands(src, portstyle.ScopeOf(src, vals.Name)) {
		name, okName := cmd.Name(src)
		if !okName || name != "checksums" || len(cmd.Words) < 2 {
			continue
		}
		seen := false
		for _, w := range cmd.Words[1:] {
			if lit, isLit := w.Literal(src); isLit && len(lit) >= 32 && strings.Contains(evaluated, lit) {
				seen = true
				break
			}
		}
		if !seen {
			ruleT = true
			// ScopeOf only descends into conditional bodies and the
			// context's own subport, so a command it found that the
			// evaluation did not take came from a branch not run.
			conditional = true
		}
	}

	// RULE D: the checksums name more distfiles than will be fetched.
	ruleD = namedFiles(vals) > len(vals.Distfiles) && len(vals.Distfiles) > 0
	return ruleT, conditional, ruleD, true
}

// namedFiles counts the distfile-keyed groups in an evaluated checksums
// list: a token that is neither a digest type nor a digest value opens a
// new group.
func namedFiles(vals info.Values) int {
	types := map[string]bool{"rmd160": true, "sha256": true, "sha1": true, "md5": true, "size": true, "sha512": true}
	n, skip := 0, false
	for _, tok := range vals.Checksums {
		switch {
		case skip:
			skip = false
		case types[tok]:
			skip = true
		default:
			n++
		}
	}
	return n
}

var _ = syntax.Script{}
