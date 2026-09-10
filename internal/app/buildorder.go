package app

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/run"
)

// buildOrder is the producer run.Spec.Requires never had, and the
// ordering that makes the field mean anything.
//
// THE WHOLE ROAD BELOW IT WAS BUILT AND NEVER FED. run.Plan's edgesFor
// translates roster positions into the built subset, EnqueueIn freezes
// the graph on the roster, verify.Request.Requires declares it, and the
// tart adapter writes a requires.<i> file per member — and no run.Spec
// literal anywhere ever set one, so every guest received nil and built
// in roster order. It is the same shape as Ask.FromSource, which was
// fed in September after refresh-checksums spent a verification proving
// its re-derived checksums against an archive built from the bytes it
// had just replaced; this is the field beside it, and it survived that
// fix pass for being beside it.
//
// ORDER IS HALF THE ANSWER AND WITHOUT IT THE GRAPH IS INERT. The guest
// runner walks positions 0..n-1 and, for each prerequisite it is told
// about, tests `[ -f state.$j ] || continue` — a prerequisite at a LATER
// position has no state file yet, so the test passes silently and the
// member builds anyway. Sorting the roster so every prerequisite comes
// first is what turns the edges into a skip.
//
// It is an optimization and never a verdict, which decides every
// failure below. No index, an unreadable one, a member the tree has
// never heard of, a cycle: each yields the roster as it stood and no
// edges, which is exactly what shipped before this existed. Nothing
// here may refuse a verification.
func buildOrder(ctx context.Context, local run.Local, roster []run.Member) ([]run.Member, [][]string) {
	if local == nil || len(roster) < 2 {
		// One member has nobody to wait for, and no local means no index
		// to ask. Both are the old behaviour, arrived at deliberately.
		return roster, nil
	}
	ports := memberNames(roster)
	declared, err := local.Requires(ctx, ports)
	if err != nil {
		slog.Debug("no build order: the tree could not answer what the members require", "err", err)
		return roster, nil
	}

	// Edges INSIDE the roster only. A dependency outside it is MacPorts'
	// to build and says nothing about the order these members go in;
	// requiresBody drops such a name at the far end anyway, and keeping
	// it here would only make the graph look bigger than it is.
	inside := make(map[string]int, len(ports))
	for i, p := range ports {
		inside[strings.ToLower(p)] = i
	}
	edges := make([][]int, len(ports))
	any := false
	for i, p := range ports {
		for _, target := range declared[strings.ToLower(p)] {
			j, ok := inside[target]
			if !ok || j == i {
				continue
			}
			edges[i] = append(edges[i], j)
			any = true
		}
	}
	if !any {
		return roster, nil
	}

	// POSITION 0 IS THE HEADLINE AND MAY NOT MOVE. run.Finish reads
	// Spec.Roster[0] as the port a cohort proposal is about, and
	// run.Observe keys the headline's manifest off it; a sort that
	// promoted a dependent would make both of them talk about the wrong
	// port. Topologically the headline is already first — the others
	// depend on IT — so this only bites where a headline requires one of
	// its own members, and there the answer is to leave the roster
	// alone rather than to reorder into a lie.
	if len(edges[0]) > 0 {
		slog.Debug("no build order: the headline requires one of its own members",
			"headline", ports[0])
		return roster, nil
	}

	order, ok := topological(edges)
	if !ok {
		// A cycle among members is a fact about the tree, not something
		// to resolve by picking an order. MacPorts builds cyclic ports
		// today by other means; this declines to have an opinion.
		slog.Debug("no build order: the members' dependencies form a cycle")
		return roster, nil
	}

	seated := make([]run.Member, 0, len(order))
	requires := make([][]string, 0, len(order))
	for _, i := range order {
		seated = append(seated, roster[i])
		names := make([]string, 0, len(edges[i]))
		for _, j := range edges[i] {
			names = append(names, ports[j])
		}
		slices.Sort(names)
		requires = append(requires, names)
	}
	slog.Debug("build order", "ports", memberNames(seated), "edges", requires)
	return seated, requires
}

// topological is a stable dependency order: prerequisites first, and
// among members that do not constrain each other, the order they came
// in. Kahn's algorithm over the lowest ready index, which is what makes
// it stable — the portdir order a roster arrives in is preserved
// wherever the graph permits it, so a cohort's members do not shuffle
// between runs for no reason a reader could see.
//
// Reports false on a cycle rather than emitting a partial order.
func topological(edges [][]int) ([]int, bool) {
	n := len(edges)
	waiting := make([]int, n) // how many prerequisites are still unplaced
	dependents := make([][]int, n)
	for i, deps := range edges {
		waiting[i] = len(deps)
		for _, j := range deps {
			dependents[j] = append(dependents[j], i)
		}
	}
	placed := make([]bool, n)
	out := make([]int, 0, n)
	for len(out) < n {
		next := -1
		for i := 0; i < n; i++ {
			if !placed[i] && waiting[i] == 0 {
				next = i
				break
			}
		}
		if next < 0 {
			return nil, false
		}
		placed[next] = true
		out = append(out, next)
		for _, d := range dependents[next] {
			waiting[d]--
		}
	}
	return out, true
}

// memberNames is the roster's ports, for a debug line.
func memberNames(ms []run.Member) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Port)
	}
	return out
}
