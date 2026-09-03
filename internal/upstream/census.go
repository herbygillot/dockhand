package upstream

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/upstream/courtesy"
)

// PortName is what to call a port that was never evaluated: the
// subport the target names, or the portdir's own base name, which is
// the convention the whole tree follows.
//
// Three rows need it and none of them can ask MacPorts. A port
// excluded by the selector filter is excluded precisely so that no
// evaluator meets it; a port the pool never reached was never
// evaluated by definition; and a port whose evaluation is what failed
// has no name from that evaluation. A row with an empty port field
// would leave the census naming nowhere to start.
func PortName(t tree.Target) string {
	if t.Subport != "" {
		return t.Subport
	}
	return filepath.Base(t.Portdir)
}

// Census counts a sweep's rows and its request budget.
//
// The two are counted together because they answer the same question
// from opposite ends: how many ports were examined, and what asking
// cost. A report that said "1623 ports examined" without saying that
// 935 round trips were made to answer them would be hiding the number
// this whole design exists to keep down — and a maintainer watching a
// paced sweep to see whether the pacing works needs the second half
// more than the first.
type Census struct {
	counts map[Outcome]int
	// budget is witness → source → count, itemized exactly as the rows
	// reported it, so the total is a sum of rows rather than a number
	// anybody has to trust.
	budget map[string]map[string]int
	total  int
	hard   int
	first  string
}

// Add counts one row.
func (c *Census) Add(r Row) {
	if c.counts == nil {
		c.counts = map[Outcome]int{}
		c.budget = map[string]map[string]int{}
	}
	c.counts[r.Outcome]++
	c.total++
	if r.Outcome.Hard() {
		if c.hard++; c.first == "" {
			c.first = r.Port
		}
	}
	for _, s := range r.Stages {
		if c.budget[s.Witness] == nil {
			c.budget[s.Witness] = map[string]int{}
		}
		c.budget[s.Witness][s.Source]++
	}
}

// Total is how many rows there were.
func (c *Census) Total() int { return c.total }

// Count is how many rows carried one outcome.
func (c *Census) Count(o Outcome) int { return c.counts[o] }

// Asked is how many round trips one witness cost, over every row.
func (c *Census) Asked(witness string) int {
	n := 0
	for src, count := range c.budget[witness] {
		if sourceAsked(src) {
			n += count
		}
	}
	return n
}

// String is the tail: what was found, then what finding it cost.
//
// Outcomes that did not occur are left out — a tail of zeroes buries
// the two lines that matter — and the witness lines are always printed
// when anything was asked, because a budget of zero is itself the
// interesting result on a rerun that hit the cache throughout.
func (c *Census) String() string {
	var b strings.Builder
	noun := "ports"
	if c.total == 1 {
		noun = "port"
	}
	fmt.Fprintf(&b, "%d %s examined\n", c.total, noun)
	if c.total == 0 {
		return b.String()
	}
	for _, o := range Outcomes {
		if n := c.counts[o]; n > 0 {
			fmt.Fprintf(&b, "  %-12s %5d  (%.1f%%)\n", o, n, 100*float64(n)/float64(c.total))
		}
	}
	for _, w := range []string{WitnessLsRemote, WitnessReleases, WitnessLivecheck} {
		if line := c.witnessLine(w); line != "" {
			b.WriteString(line)
		}
	}
	if n := c.counts[OutcomeWalled]; n > 0 {
		// The one thing the reader of a walled report needs, said rather
		// than inferred. A wall lasts a quarter of an hour and a paced
		// sweep of a whole tree takes longer than that, so a wall raised
		// early turns thousands of ports into walled rows and then
		// quietly resumes — and the exit status is 0, correctly, because
		// a host refusing us is not a broken port. A person reading "45%
		// walled" should not have to work out that those ports were not
		// examined and that running again finishes them.
		fmt.Fprintf(&b, "  %d port(s) were not examined: a host refused dockhand and was left alone. Run again later to finish them.\n", n)
	}
	return b.String()
}

// witnessLine reports one witness's budget: how many round trips it
// cost, and the breakdown of where every consultation came from.
func (c *Census) witnessLine(witness string) string {
	sources := c.budget[witness]
	if len(sources) == 0 {
		return ""
	}
	names := make([]string, 0, len(sources))
	for s := range sources {
		names = append(names, s)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, s := range names {
		parts = append(parts, fmt.Sprintf("%d %s", sources[s], s))
	}
	return fmt.Sprintf("  %-12s %5d asked  (%s)\n", witness, c.Asked(witness), strings.Join(parts, ", "))
}

// sourceAsked reports whether a row's recorded source cost a round
// trip. It reads the word rather than the type because that is what a
// row carries: the source is a string on the wire so that a reader of
// the raw stream needs no table, and this is the one place that has to
// turn it back into the fact.
//
// Two words cost nothing, for two different reasons: fresh is an
// observation held from an earlier run, and shared is the round trip
// another worker was already paying for the same repository. Counting
// either would report a budget larger than the one actually spent.
func sourceAsked(src string) bool {
	return src != courtesy.Fresh.String() && src != courtesy.Shared.String()
}

// Err is the sweep's exit: nil when every port was answered, and a
// SweepError when any of them could not be examined at all.
func (c *Census) Err() error {
	if c.hard == 0 {
		return nil
	}
	return &SweepError{Hard: c.hard, Total: c.total, First: c.first}
}

// SweepError is a sweep that finished with rows that were not answers.
//
// It is a type rather than a count returned alongside because the
// contract of a report over many ports is that $? answers without
// anybody reading the report: 0 when every port was examined, 83 when
// some of them were not. A sweep that exited 0 over four hundred ports
// it never reached would say the tree was surveyed when it was not.
//
// Being outdated is not in it, and neither is a host that refused us.
// The first is the report's subject and the second is somebody else's
// problem — both are answers, and a report whose exit status changed
// because upstream had shipped something would be unusable in the one
// place an exit status is read.
type SweepError struct {
	// Hard is how many rows were not answers.
	Hard int
	// Total is how many rows there were.
	Total int
	// First is the first such row's port, so the message names
	// somewhere to start.
	First string
}

func (e *SweepError) Error() string {
	return fmt.Sprintf("%d of %d ports could not be examined, starting at %s",
		e.Hard, e.Total, e.First)
}

// DockhandExit: the partial band. Ports were examined and some of them
// could not be, which is neither success nor a failure of the whole
// run.
func (e *SweepError) DockhandExit() int { return exitcode.SweepHardErrors }

// Code names the outcome for a machine.
func (e *SweepError) Code() string { return "sweep-hard-errors" }
