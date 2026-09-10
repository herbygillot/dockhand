package bump

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/port/porttest"
)

// TestConfirmProbeSurvey is a MEASUREMENT HARNESS and not a test of the
// shipped tree. It asks one question of the real discover: over a list
// of ports, where does the affix arithmetic prove a carrier that the
// confirming evaluation then refuses to deliver?
//
// The answer is the cost of the confirm. Every port it names is one
// discovery would have planned on and got wrong; every port it does not
// name pays one extra 17ms evaluation and loses nothing.
//
// Run it deliberately:
//
//	DOCKHAND_CONFIRM_SURVEY=/path/to/ports \
//	DOCKHAND_CONFIRM_PORTS=<file of portdir paths, one per line> \
//	  go test ./internal/intent/bump/ -run TestConfirmProbeSurvey -timeout 60m -v
func TestConfirmProbeSurvey(t *testing.T) {
	list := os.Getenv("DOCKHAND_CONFIRM_PORTS")
	if list == "" {
		t.Skip("set DOCKHAND_CONFIRM_PORTS=<file of portdir paths, one per line>")
	}
	raw, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}
	ev := porttest.Evaluator(t)
	ctx := context.Background()

	var proven, refused, none int
	for _, dir := range strings.Fields(string(raw)) {
		h := porttest.Handle(ev, dir)
		src, cst, err := h.Source()
		if err != nil {
			continue
		}
		vals, err := h.Values(ctx)
		if err != nil || vals.Version == "" {
			continue
		}
		target := nextVersion(vals.Version)
		if target == "" {
			continue
		}
		c, ok := discover(ctx, h, src, cst, vals, target)
		switch {
		case ok:
			proven++
			fmt.Fprintf(os.Stderr, "proven   %-44s %s -> %s  (%s)\n", dir, vals.Version, c.Result.Version, c.Template)
		default:
			// Distinguish "the arithmetic never proved anything" from
			// "it proved a carrier the confirm threw out": the second
			// is the population this measurement exists to count.
			if len(prove(ctx, h, src, cst, vals, target)) == 1 {
				refused++
				fmt.Fprintf(os.Stderr, "REFUSED  %-44s %s -> %s\n", dir, vals.Version, target)
			} else {
				none++
				fmt.Fprintf(os.Stderr, "none     %-44s %s -> %s  |%s|\n", dir, vals.Version, target,
					elsewhere(ctx, h, src, cst, vals))
			}
		}
	}
	fmt.Fprintf(os.Stderr, "\n== proven %d | refused by confirm %d | never proven %d ==\n", proven, refused, none)
}

// nextVersion bumps the last numeric run of a version, which is enough
// of a target for a survey that only cares whether a carrier delivers.
func nextVersion(v string) string {
	i := len(v)
	for i > 0 && v[i-1] >= '0' && v[i-1] <= '9' {
		i--
	}
	if i == len(v) {
		return ""
	}
	n := 0
	for _, c := range v[i:] {
		n = n*10 + int(c-'0')
	}
	return fmt.Sprintf("%s%d", v[:i], n+1)
}
