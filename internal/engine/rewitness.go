package engine

// Re-witnessing: fetching a change's distfiles again, from upstream
// alone, immediately before an unattended pass would publish it.
//
// The event this exists to catch is the one dockhand's own refresh verb
// exists to catch, arriving at the worst possible moment. A change is
// minted, its checksums are recorded from bytes upstream served an hour
// or a week ago, and it sits in the namespace waiting for a verdict.
// Between the mint and the publication upstream re-rolls the artifact at
// the same version. Nothing local moves — that is what makes a stealth
// update a stealth update — so every local reading still agrees with
// itself, and the pull request an unattended pass would open would carry
// checksums for bytes that no longer exist, vouched for by a machine.
//
// A person promoting is a different case and is not covered here: they
// are looking at the change, they typed the verb, and the refresh verb
// is theirs to run. The machine has neither the judgment nor the excuse,
// so its road pays for one more fetch.
//
// WHAT A MISMATCH DOES IS HOLD, NEVER DECIDE. A re-witness that differs
// is a suspicion and not a finding of wrongdoing: an upstream may have
// rebuilt a tarball for entirely dull reasons. So the change is held,
// the suspicion is appended as a proposed finding with the two sums in
// it, and a person is asked. Nothing is edited, nothing is discarded,
// and the false positive costs a hold rather than a wrong change — which
// is what lets the comparison below be deliberately broad.

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/record"
)

// StealthSuspected is the finding kind a re-witness appends when the
// bytes upstream serves no longer hash to what the change records.
//
// A wire word, so it is spelled once. The kind is what `dockhand status`
// prints and what `dockhand dismiss` answers, and a second spelling of
// it would be a second finding as far as every reader is concerned.
const StealthSuspected = "stealth-suspected"

// ChecksumMismatch is one recorded value the re-fetch contradicts.
type ChecksumMismatch struct {
	File string
	Type string
	// Recorded is what the change's Portfile says; Served is what
	// upstream just handed back.
	Recorded string
	Served   string
}

func (m ChecksumMismatch) String() string {
	return fmt.Sprintf("%s %s: recorded %s, upstream now serves %s", m.File, m.Type, m.Recorded, m.Served)
}

// rewitness re-derives a change's checksums from what upstream serves
// now and reports every recorded value the fetch contradicts.
//
// The fetch disables the mirrors, and that switch is the whole point:
// the MacPorts mirrors hold the bytes the recorded checksums were made
// from, so a fetch that may fall back to them can answer "unchanged"
// about an upstream that re-rolled. The question is what upstream serves
// NOW, and only upstream can answer it.
//
// The change's own tree is what gets evaluated, materialized out of the
// branch at its tip: the working copy is on some other branch, and
// evaluating that would re-witness a different change's checksums.
func (e *Engine) rewitness(ctx context.Context, repo *git.Repo, tip string, n record.Record) ([]ChecksumMismatch, error) {
	portdirs := n.Portdirs()
	if len(portdirs) == 0 {
		// A record naming no portdir cannot be re-witnessed, and saying so
		// is not the same as saying it is clean. The caller treats the
		// error as a refusal, which is the reading that does not publish.
		return nil, fmt.Errorf("re-witness: %s names no portdir", git.Abbrev(tip))
	}
	// The three seams this road needs, named before any of them is
	// called.
	//
	// A nil seam is a composition bug and it deserves a sentence rather
	// than a stack trace from the middle of a publish road. VerifyProvider
	// and RunGH already refuse plainly when nobody wired them; these three
	// had no equivalent, so an Engine assembled without them segfaulted
	// inside the last gate before a push — the worst place in the tree to
	// learn about a wiring mistake, and the one place a caller cannot tell
	// a crash from a refusal.
	for _, seam := range []struct {
		what    string
		missing bool
	}{
		{"an evaluator session", e.Session == nil},
		{"a temporary root", e.Temp == nil},
		{"a distfile fetcher", e.Fetch == nil},
	} {
		if seam.missing {
			return nil, fmt.Errorf("re-witness: %s is not wired into this run", seam.what)
		}
	}
	// A session of its own, closed here: it evaluates materialized copies
	// under temporary directories this function removes, and the run's
	// own evaluator would outlive them.
	ev, err := e.Session(ctx)
	if err != nil {
		return nil, err
	}
	defer ev.Close()

	root, err := e.Temp()
	if err != nil {
		return nil, err
	}
	fetcher, err := e.Fetch(ctx)
	if err != nil {
		return nil, err
	}

	var all []ChecksumMismatch
	for _, rel := range portdirs {
		found, err := e.rewitnessOne(ctx, repo, tip, rel, ev, root, fetcher)
		if err != nil {
			return nil, err
		}
		all = append(all, found...)
	}
	return all, nil
}

// rewitnessOne re-witnesses one portdir of the change.
func (e *Engine) rewitnessOne(ctx context.Context, repo *git.Repo, tip, rel string,
	ev port.Oracle, root interface {
		MakeDir(string) (string, func(), error)
	}, fetcher fetchSurface) ([]ChecksumMismatch, error) {
	stage, removeStage, err := root.MakeDir("rewitness-port")
	if err != nil {
		return nil, err
	}
	defer removeStage()
	if err := repo.Materialize(ctx, tip, rel, stage); err != nil {
		return nil, err
	}
	h := port.New(tree.Target{Portdir: filepath.Join(stage, filepath.FromSlash(rel))}, ev)
	vals, err := h.Values(ctx)
	if err != nil {
		return nil, fmt.Errorf("re-witness: evaluating %s at %s: %w", rel, git.Abbrev(tip), err)
	}
	recorded, err := checksums.Parse(vals.Checksums)
	if err != nil {
		return nil, fmt.Errorf("re-witness: %s: %w", rel, err)
	}
	if len(recorded) == 0 {
		// A port with no recorded checksums fetches nothing — a
		// meta-port, a fetch-free port. There is nothing to contradict.
		return nil, nil
	}
	fi, err := h.FetchInfo(ctx, true)
	if err != nil {
		return nil, err
	}
	fetchDir, removeFetched, err := root.MakeDir("rewitness-distfiles")
	if err != nil {
		return nil, err
	}
	defer removeFetched()

	opts := distfile.Options{
		DisableEPSV:   fi.DisableEPSV,
		IgnoreSSLCert: fi.IgnoreSSLCert,
		UserAgent:     fi.UserAgent,
	}
	got := make(map[string]checksums.Sums, len(fi.Files))
	for _, file := range recordedFiles(recorded, fi.Files) {
		s, err := fetcher.Fetch(ctx, fi.Files[file], opts, filepath.Join(fetchDir, file))
		if err != nil {
			// A fetch that cannot complete is not evidence of a stealth
			// update, and it is not evidence of anything else either. It
			// stops the publication because the check did not run, which is
			// the only reading that does not publish on an unproven claim.
			return nil, fmt.Errorf("re-witness: %s: %w", file, err)
		}
		got[file] = s
	}
	return CompareRecorded(recorded, got), nil
}

// fetchSurface is the fetching half of the re-witness, named so the
// comparison can be exercised without a network.
type fetchSurface interface {
	Fetch(ctx context.Context, urls []string, opts distfile.Options, dest string) (checksums.Sums, error)
}

// recordedFiles lists the distfiles to re-fetch: the ones the change's
// own checksums name AND the fetch surface offers a url for, sorted so
// two passes fetch in the same order.
//
// The single-distfile form records no filename, which is why the
// surface's own name is used when exactly one file is offered. A
// recorded name the surface does not offer is skipped rather than
// refused: a distfile supplied by a vendored block is recorded in the
// block and served by nothing this evaluation can see, and a re-witness
// that refused over one would hold every port that carries a block.
func recordedFiles(recorded []checksums.Recorded, offered map[string][]string) []string {
	want := map[string]bool{}
	for _, r := range recorded {
		switch {
		case r.File != "":
			want[r.File] = true
		case len(offered) == 1:
			for file := range offered {
				want[file] = true
			}
		}
	}
	out := make([]string, 0, len(want))
	for file := range want {
		if _, ok := offered[file]; ok {
			out = append(out, file)
		}
	}
	sort.Strings(out)
	return out
}

// CompareRecorded is the judgment half: every recorded value the fetched
// bytes contradict, in a stable order.
//
// A recorded type this build cannot compute — the legacy md5 and sha1
// still sitting in ancient Portfiles — is skipped rather than reported.
// Nothing here can produce a value to compare it against, and a
// mismatch invented out of an empty string would hold every one of
// those ports on the first unattended pass.
//
// A file the fetch did not produce is likewise not a mismatch. It is a
// gap, the caller has already refused over it, and reporting it here as
// a contradiction would put a stealth-update finding on a note over a
// timeout.
func CompareRecorded(recorded []checksums.Recorded, got map[string]checksums.Sums) []ChecksumMismatch {
	var out []ChecksumMismatch
	single := ""
	if len(got) == 1 {
		for file := range got {
			single = file
		}
	}
	for _, r := range recorded {
		file := r.File
		if file == "" {
			file = single
		}
		sums, ok := got[file]
		if !ok {
			continue
		}
		served, computable := sums.Value(r.Type)
		if !computable {
			continue
		}
		if served == r.Value {
			continue
		}
		out = append(out, ChecksumMismatch{File: file, Type: r.Type, Recorded: r.Value, Served: served})
	}
	return out
}

// StealthFinding is the proposed finding a mismatch appends, and the
// hold that goes with it.
//
// Proposed, not accepted: the finding says what was measured and asks,
// which is the same contract every other finding in the tree has. A
// person answers it with `dockhand dismiss` after establishing why the
// artifact moved, or with a refresh that re-derives the sums and a new
// verification over them.
func StealthFinding(ports []string, mismatch []ChecksumMismatch, at time.Time) record.Finding {
	lines := make([]string, 0, len(mismatch))
	for _, m := range mismatch {
		lines = append(lines, m.String())
	}
	return record.Finding{
		Kind:  StealthSuspected,
		Ports: ports,
		// The criterion is the measurement in words a reader can check: a
		// finding that cannot be traced back to two numbers is an
		// assertion, and this one is asking a person to distrust an
		// upstream.
		Criterion: "the distfiles were fetched again from upstream with the MacPorts mirrors disabled, and " +
			joinLines(lines),
		Source:      "upstream, re-fetched at publication time",
		Disposition: record.Proposed,
		At:          at,
	}
}

// joinLines renders the mismatches as one sentence.
func joinLines(lines []string) string {
	switch len(lines) {
	case 0:
		return "nothing differed"
	case 1:
		return lines[0]
	}
	out := lines[0]
	for _, l := range lines[1:] {
		out += "; " + l
	}
	return out
}
