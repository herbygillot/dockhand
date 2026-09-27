package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
)

// assessed is the part of a journal line a comparison reads. It is its own
// type, not Port, so journals written by older builds still load.
type assessed struct {
	Selector string
	Outcome  string
	Portfile string
	Findings []struct{ Check, Status, Code, Detail string }
	Coverage []struct {
		Fetch *struct {
			Guards  []string
			Problem string
		}
	}
}

// quality orders outcomes from best to worst, for what counts as a
// regression.
var quality = map[string]int{"ready": 0, "candidate-ready": 0, "input-found": 0, "unknown": 1, "unsupported": 2, "blocked": 3}

func loadJournal(path string) (map[string]assessed, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	ports := map[string]assessed{}
	commit := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		var source struct{ Source *struct{ Commit string } }
		if json.Unmarshal(line, &source) == nil && source.Source != nil {
			commit = source.Source.Commit
			continue
		}
		var port assessed
		if err := json.Unmarshal(line, &port); err != nil {
			return nil, "", fmt.Errorf("%s: %w", path, err)
		}
		ports[port.Selector] = port
	}
	return ports, commit, scanner.Err()
}

// firstStop is the first finding that stopped a port, as the baseline
// survey bucketed them.
func (p assessed) firstStop() string {
	for _, f := range p.Findings {
		switch f.Status {
		case "unsupported", "unknown", "blocked":
			return f.Check + "/" + f.Code
		}
	}
	return "(no stopping finding)"
}

func (p assessed) findings() string {
	var all []string
	for _, f := range p.Findings {
		all = append(all, f.Check+":"+f.Status+":"+f.Code)
	}
	sort.Strings(all)
	return strings.Join(all, " ")
}

func (p assessed) guards() (guards []string, refused bool) {
	for _, c := range p.Coverage {
		if c.Fetch == nil {
			continue
		}
		guards = append(guards, c.Fetch.Guards...)
		refused = refused || c.Fetch.Problem != ""
	}
	sort.Strings(guards)
	return slices.Compact(guards), refused
}

// compareJournals reports how a survey differs from its baseline over the
// ports both hold: outcomes and their moves, regressions, findings,
// fetch guards, Portfiles fully covered, and where ports stop.
func compareJournals(out io.Writer, basePath, newPath string, top int) error {
	base, baseCommit, err := loadJournal(basePath)
	if err != nil {
		return err
	}
	next, nextCommit, err := loadJournal(newPath)
	if err != nil {
		return err
	}
	var shared []string
	onlyBase, onlyNew := 0, 0
	for selector := range base {
		if _, ok := next[selector]; ok {
			shared = append(shared, selector)
		} else {
			onlyBase++
		}
	}
	for selector := range next {
		if _, ok := base[selector]; !ok {
			onlyNew++
		}
	}
	sort.Strings(shared)
	fmt.Fprintf(out, "baseline %s at %s: %d ports\nnew      %s at %s: %d ports\nshared %d; only in the baseline %d; only in the new %d\n",
		basePath, shortID(baseCommit), len(base), newPath, shortID(nextCommit), len(next), len(shared), onlyBase, onlyNew)

	before, after := map[string]int{}, map[string]int{}
	moves := map[string]int{}
	var regressions, improvements []string
	findingChanges := map[string]int{}
	changedFindings, guardsLost, guardsChanged, refusedAccepted, acceptedRefused := 0, 0, 0, 0, 0
	baseStops, newStops := map[string]int{}, map[string]int{}
	for _, selector := range shared {
		b, n := base[selector], next[selector]
		before[b.Outcome]++
		after[n.Outcome]++
		if b.Outcome != n.Outcome {
			moves[b.Outcome+" → "+n.Outcome]++
			line := fmt.Sprintf("%s: %s → %s", selector, b.Outcome, n.Outcome)
			if quality[n.Outcome] > 0 {
				line += " (" + n.firstStop() + ")"
			}
			if quality[n.Outcome] > quality[b.Outcome] {
				regressions = append(regressions, line)
			} else {
				improvements = append(improvements, line)
			}
		}
		if bf, nf := b.findings(), n.findings(); bf != nf {
			changedFindings++
			findingChanges[diffWords(bf, nf)]++
		}
		bg, bRefused := b.guards()
		ng, nRefused := n.guards()
		switch {
		case len(bg) > 0 && len(ng) == 0:
			guardsLost++
		case len(bg) > 0 && !slices.Equal(bg, ng):
			guardsChanged++
		}
		switch {
		case bRefused && !nRefused:
			refusedAccepted++
		case !bRefused && nRefused:
			acceptedRefused++
		}
		if quality[b.Outcome] > 0 {
			baseStops[b.firstStop()]++
		}
		if quality[n.Outcome] > 0 {
			newStops[n.firstStop()]++
		}
	}

	fmt.Fprintln(out, "\noutcomes over the shared ports")
	for _, outcome := range sortedKeys(before, after) {
		fmt.Fprintf(out, "  %-16s %6d → %6d  (%+d)\n", outcome, before[outcome], after[outcome], after[outcome]-before[outcome])
	}
	fmt.Fprintln(out, "\nmoves")
	for _, move := range byCount(moves, 0) {
		fmt.Fprintf(out, "  %6d  %s\n", moves[move], move)
	}
	fmt.Fprintf(out, "\nregressions: %d\n", len(regressions))
	for _, line := range head(regressions, top) {
		fmt.Fprintln(out, "  "+line)
	}
	fmt.Fprintf(out, "improvements: %d\n", len(improvements))
	for _, line := range head(improvements, top) {
		fmt.Fprintln(out, "  "+line)
	}
	fmt.Fprintf(out, "\nports whose findings changed: %d\n", changedFindings)
	for _, change := range byCount(findingChanges, top) {
		fmt.Fprintf(out, "  %6d  %s\n", findingChanges[change], change)
	}
	fmt.Fprintf(out, "\nfetch guards lost %d, changed %d; refused then accepted %d, accepted then refused %d\n", guardsLost, guardsChanged, refusedAccepted, acceptedRefused)
	fmt.Fprintf(out, "Portfiles fully covered: %d → %d\n", fullyCovered(base, shared), fullyCovered(next, shared))
	fmt.Fprintln(out, "\nwhere ports stop (first stopping finding)")
	for _, stop := range byCount(merge(baseStops, newStops), top) {
		fmt.Fprintf(out, "  %6d → %6d  %s\n", baseStops[stop], newStops[stop], stop)
	}
	return nil
}

// fullyCovered counts the Portfiles all of whose shared ports found their
// version input.
func fullyCovered(ports map[string]assessed, shared []string) int {
	covered := map[string]bool{}
	for _, selector := range shared {
		p := ports[selector]
		if _, seen := covered[p.Portfile]; !seen {
			covered[p.Portfile] = true
		}
		covered[p.Portfile] = covered[p.Portfile] && p.Outcome == "input-found"
	}
	count := 0
	for _, all := range covered {
		if all {
			count++
		}
	}
	return count
}

// diffWords names what a change removed and added, finding by finding.
func diffWords(before, after string) string {
	b, a := strings.Fields(before), strings.Fields(after)
	var removed, added []string
	for _, f := range b {
		if !slices.Contains(a, f) {
			removed = append(removed, "-"+f)
		}
	}
	for _, f := range a {
		if !slices.Contains(b, f) {
			added = append(added, "+"+f)
		}
	}
	return strings.Join(append(removed, added...), " ")
}

func sortedKeys(maps_ ...map[string]int) []string {
	keys := map[string]bool{}
	for _, m := range maps_ {
		for k := range m {
			keys[k] = true
		}
	}
	return slices.Sorted(maps.Keys(keys))
}

func merge(a, b map[string]int) map[string]int {
	all := map[string]int{}
	for k, v := range a {
		all[k] += v
	}
	for k, v := range b {
		all[k] += v
	}
	return all
}

// byCount lists a count map's keys, largest first, at most n unless n is 0.
func byCount(counts map[string]int, n int) []string {
	keys := slices.Collect(maps.Keys(counts))
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if n > 0 {
		return head(keys, n)
	}
	return keys
}

func head(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	return lines
}
