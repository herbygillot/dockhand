package classify

import (
	"fmt"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/portstyle"
)

// Census aggregates classification results into the survey the command
// reports: totals by outcome, and by style for the located.
type Census struct {
	Total     int
	ByOutcome map[Outcome]int
	ByStyle   map[portstyle.Type]int
	// GoMinDeclared counts ports declaring go.toolchain_min, the Go
	// floor bump maintains.
	GoMinDeclared int
}

// Add folds one result into the census.
func (c *Census) Add(r Result) {
	if c.ByOutcome == nil {
		c.ByOutcome = map[Outcome]int{}
		c.ByStyle = map[portstyle.Type]int{}
	}
	c.Total++
	c.ByOutcome[r.Outcome]++
	if r.Outcome == Located {
		c.ByStyle[r.Style]++
	}
	if r.DeclaresGoMin {
		c.GoMinDeclared++
	}
}

// labelWidth is the column a list of labels needs: the widest of them,
// never narrower than the width this report was written at.
//
// It WAS that width, as a literal 14 — and "bitbucket.setup" and
// "sourcehut.setup" are fifteen, so those two rows pushed their counts
// one place right and the count column stopped being a column. Read off
// the data it cannot go stale the next time a style is named, which is
// the only reason to compute a constant.
func labelWidth(n int, at func(int) string) int {
	w := 14
	for i := range n {
		if l := len(at(i)); l > w {
			w = l
		}
	}
	return w
}

// String renders the census as a small fixed-order report.
func (c *Census) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d ports classified\n", c.Total)
	outcomes := []Outcome{Located, Probeable, NotLiteral, UnknownStyle, ParseFailed, EvalFailed}
	ow := labelWidth(len(outcomes), func(i int) string { return outcomes[i].String() })
	for _, o := range outcomes {
		if n := c.ByOutcome[o]; n > 0 {
			fmt.Fprintf(&b, "  %-*s %5d  (%.1f%%)\n", ow, o, n, 100*float64(n)/float64(c.Total))
		}
	}
	if c.GoMinDeclared > 0 {
		fmt.Fprintf(&b, "  go.toolchain_min declared: %d\n", c.GoMinDeclared)
	}
	if len(c.ByStyle) > 0 {
		b.WriteString("located by style:\n")
		type row struct {
			t portstyle.Type
			n int
		}
		var rows []row
		for t, n := range c.ByStyle {
			rows = append(rows, row{t, n})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].n != rows[j].n {
				return rows[i].n > rows[j].n
			}
			return rows[i].t < rows[j].t
		})
		sw := labelWidth(len(rows), func(i int) string { return rows[i].t.String() })
		for _, r := range rows {
			fmt.Fprintf(&b, "  %-*s %5d\n", sw, r.t, r.n)
		}
	}
	return b.String()
}
