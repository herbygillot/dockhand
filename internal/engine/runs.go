package engine

import (
	"fmt"
	"os"
	"sort"

	"github.com/herbygillot/dockhand/internal/record"
)

// Reading schema 3's two maps back. A record keeps one job per
// release and one run per subject per release, so every verb that acts
// on a run — settling it, canceling it, retrying it — needs both names
// back and the job the run was living in.
//
// The names are reached through the subjects and the run's own
// platform, never by splitting the run's key. The key is a join of two
// values the record already holds apart, and a verb that re-parsed it
// would be guessing at something it was handed.

// runRef is one run with everything that identifies it: the subject it
// is about, the release it ran on, and the directory that subject
// contributed to the build.
type runRef struct {
	Port    string
	Portdir string
	Release string
	Run     record.Run
}

// Key is where this run sits in the record.
func (r runRef) Key() string { return record.RunKey(r.Port, r.Release) }

// runRefs walks a record's runs in a stable order: subjects in build
// order, releases sorted within each. Two reports of one record name
// its platforms in the same order because of this and not by accident.
func runRefs(n record.Record) []runRef {
	rels := releasesIn(n)
	out := make([]runRef, 0, len(n.Runs))
	for _, s := range n.Subjects {
		for _, rel := range rels {
			run, ok := n.Runs[record.RunKey(s.Port, rel)]
			if !ok {
				continue
			}
			out = append(out, runRef{Port: s.Port, Portdir: s.Portdir, Release: rel, Run: run})
		}
	}
	return out
}

// runsOn narrows the same walk to one release: every subject's verdict
// inside one guest.
func runsOn(n record.Record, release string) []runRef {
	var out []runRef
	for _, ref := range runRefs(n) {
		if ref.Release == release {
			out = append(out, ref)
		}
	}
	return out
}

// releasesIn lists every release a record knows about, sorted: the
// jobs' own keys, and the platforms its runs name.
//
// The union, and deliberately not Record.Platforms. Platforms answers
// what the change was submitted to, which is the right answer to a
// different question: a queued run was never submitted and a run
// carried over from the pre-mint gate was earned in an environment
// already handed back, so both exist with no job behind them. A verb
// looking for work to do must see those; a report saying which
// environments were entered must not.
func releasesIn(n record.Record) []string {
	seen := make(map[string]bool, len(n.Jobs))
	for rel := range n.Jobs {
		seen[rel] = true
	}
	for _, run := range n.Runs {
		if run.Platform != "" {
			seen[run.Platform] = true
		}
	}
	out := make([]string, 0, len(seen))
	for rel := range seen {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// keepsEnvironment reports whether a job still holds something a
// person could enter: it named a handle and has not been given back.
//
// Both halves are needed because a release does not erase the name.
// The handle says what was handed back, which is what a failed release
// leaves a person to go and delete by hand, so it outlives the flag
// that says the handing back happened.
func keepsEnvironment(j record.JobRecord) bool { return j.Handle != "" && !j.Released }

// holdsEnvironment reports whether a record still holds any kept debug
// environment — the failure side's counterpart to a running run.
func holdsEnvironment(n record.Record) bool {
	for _, j := range n.Jobs {
		if keepsEnvironment(j) {
			return true
		}
	}
	return false
}

// anyRunning reports whether any run on a release is still using its
// guest.
func anyRunning(n record.Record, release string) bool {
	for _, ref := range runsOn(n, release) {
		if ref.Run.State == record.Running {
			return true
		}
	}
	return false
}

// claimant names this session as the owner of a submission: the host
// and the process id.
//
// It is as much identity as a machine-local note can honestly carry,
// and it is enough for the thing the claim is for — telling two
// dockhands that share a checkout apart. A hostname that cannot be
// read is written as "unknown" rather than left empty, because an
// empty owner reads as an unclaimed job and this one is claimed.
func claimant() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s/%d", host, os.Getpid())
}
