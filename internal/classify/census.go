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
}

// String renders the census as a small fixed-order report.
func (c *Census) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d ports classified\n", c.Total)
	for _, o := range []Outcome{Located, Probeable, NotLiteral, UnknownStyle, ParseFailed, EvalFailed} {
		if n := c.ByOutcome[o]; n > 0 {
			fmt.Fprintf(&b, "  %-14s %5d  (%.1f%%)\n", o, n, 100*float64(n)/float64(c.Total))
		}
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
		for _, r := range rows {
			fmt.Fprintf(&b, "  %-14s %5d\n", r.t, r.n)
		}
	}
	return b.String()
}
