package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
)

// inspect observes one branch: the tip, its settled note (nil when
// unnoted), and the drift finding that stands in for a note when there
// is none.
//
// This is the reconciler's whole reading of a branch, and it is
// deliberately the only one: both renderings draw on the same three
// values, so there is no way for the human report and the machine one
// to disagree about what a branch is doing. The wording is render's,
// and the clock a running run's elapsed time is measured against is
// read by the pass rather than in here, so a golden can pin the
// sentence.
func (e *Engine) inspect(ctx context.Context, repo *git.Repo, branch string) (string, *record.Record, string, error) {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return "", nil, "", err
	}
	n, err := e.Ledger(repo).Read(ctx, tip)
	if errors.Is(err, git.ErrNoNote) {
		drift, derr := e.describeUnverifiedTip(ctx, repo, branch, tip)
		if derr != nil {
			return tip, nil, "", derr
		}
		return tip, nil, drift, nil
	}
	if err != nil {
		return tip, nil, "", err
	}
	if n.AnyState(record.Running) {
		if err := e.settle(ctx, repo, &n); err != nil {
			return tip, nil, "", err
		}
	}
	return tip, &n, "", nil
}

// settle polls every running run and writes what it learns back to the
// note. Poll never mutates and Release is the caller's: status
// releases the worker on pass — a kept green environment is a wasted
// slot — and keeps it on failure, where it is the debug handle. A
// failure whose log shows the port refusing the platform records as
// unsupported instead, and its worker is released: a correct refusal
// leaves nothing to debug.
//
// The polling happens outside the notes lock and only the writing
// inside it. That is the split a reconciler needs: a poll is a round
// trip per run and a log fetch is another, and holding the flock across
// them stalls every peer sharing the checkout for as long as the
// slowest provider takes. What it costs is that the note can move while
// this pass is asking, and the compare below is what pays for it — a
// judgment is written only if the run it was reached from is still the
// run on the note, state and job both. Two agents share a checkout now,
// and a cancel that lands mid-poll must not come back as passed.
//
// A dropped judgment has already released its worker, which is the
// honest order: the run it was judging is over either way, and the
// claimant that moved the note released the same worker or is about to.
func (e *Engine) settle(ctx context.Context, repo *git.Repo, n *record.Record) error {
	prov, err := e.Verifier(ctx)
	if err != nil {
		return nil // running, cannot poll; the note stands as is
	}
	// judged is what this pass concluded, and from is the run each
	// conclusion was reached from — the two halves of the compare.
	judged, from := map[string]record.Run{}, map[string]record.Run{}
	for plat, r := range n.Runs {
		if r.State != record.Running {
			continue
		}
		in := verdict.RunInput{Run: r, Port: n.Port}
		st, perr := prov.Poll(ctx, r.Job)
		switch {
		case errors.Is(perr, verify.ErrUnknownJob):
			// The job is gone, and so is the worker: nothing to read and
			// nothing to release.
			in.Vanished = true
		case perr != nil:
			// A provider that cannot answer settles nothing at all: the
			// runs judged before this one are left unwritten too, because
			// a half-settled note is a worse account than an unsettled
			// one.
			return perr
		default:
			in.Status = st
			// The log is fetched before the release, because releasing a
			// worker puts its log out of reach — and only when the
			// judgment will actually read one.
			if verdict.NeedsLog(st.State, r.Linted) {
				if log, lerr := prov.Log(ctx, r.Job); lerr == nil {
					in.Log, in.LogRead = log, true
				}
			}
			// Whether a blamed dependency has a maintainer is a fact
			// about the tree, which a judgment cannot go and read. The
			// guarded reader answers whether it is even worth looking,
			// so a port that merely declined the platform sends nobody
			// globbing.
			if st.State == verify.Failed && in.LogRead {
				if dep, ok := verdict.BlamedDependency(in.Log, n.Port); ok {
					in.Nomaintainer = nomaintainerDep(repo.Root, dep)
				}
			}
		}
		j := verdict.JudgeRun(in)
		if !j.Settled {
			continue
		}
		switch j.Release {
		case verdict.KeepWorker:
		case verdict.ReleaseAndReport:
			j = j.AfterRelease(prov.Release(ctx, r.Job))
		case verdict.ReleaseQuietly:
			// Nothing waits on this one, so it runs on a context that
			// survives our own cancellation and its answer goes nowhere.
			_ = prov.Release(context.WithoutCancel(ctx), r.Job)
		}
		judged[plat], from[plat] = j.Run, r
	}
	if len(judged) == 0 {
		return nil
	}
	// The write is the ledger's own read-modify-write, which re-reads
	// under the flock. A run whose state moved since it was observed was
	// settled, canceled or superseded by somebody who saw a note this
	// pass did not, so this pass's word about it is dropped rather than
	// merged; a pass that lands none of its judgments writes nothing at
	// all, which is what keeps a poll from adding a notes object.
	//
	// The compare is on the run's identity and not on its state word
	// alone, because the word alone cannot see a run that came back. A
	// peer that cancels the run and starts another leaves the platform
	// reading running again, and a compare that only asked for "running"
	// would write this pass's verdict about the canceled job over the
	// live one: a verdict the user never asked for, and a worker no note
	// names any more.
	var settled *record.Record
	if err := e.Ledger(repo).Update(ctx, n.Sha, n.Port, func(fresh *record.Record) error {
		applied := false
		for plat, run := range judged {
			was, observed := fresh.Runs[plat], from[plat]
			if was.State != observed.State || was.Job.ID != observed.Job.ID {
				continue
			}
			fresh.Runs[plat] = run
			applied = true
		}
		// The caller's copy becomes what the note says, so that a dropped
		// judgment leaves the reader looking at the peer's record rather
		// than at the poll this pass threw away. The exception is a note
		// that is no longer there: a peer's discard removes it mid-pass,
		// LoadOrStart mints a record for the commit, and handing that back
		// would describe a deleted branch as noted with no runs. Every run
		// judged here was running when it was observed, so a record that
		// knows none of them is not the note being settled.
		if knowsAny(*fresh, judged) {
			cur := *fresh
			settled = &cur
		}
		if !applied {
			return ledger.ErrUnchanged
		}
		return nil
	}); err != nil {
		return err
	}
	if settled != nil {
		*n = *settled
	}
	return nil
}

// knowsAny reports whether a record carries a run for any of the
// platforms judged — the test of whether it is the note those
// judgments were made about at all.
func knowsAny(n record.Record, judged map[string]record.Run) bool {
	for plat := range judged {
		if _, ok := n.Runs[plat]; ok {
			return true
		}
	}
	return false
}

// nomaintainerDep reports whether a blamed dependency's Portfile says
// nomaintainer — the one tree read a settlement makes, kept out of the
// judgment that uses it. The glob covers one category level and wants
// exactly one match: two categories carrying the same port name name
// nobody in particular. A port that cannot be found is simply not
// annotated, which reads the same as a maintained one, and both mean
// say nothing.
func nomaintainerDep(treeRoot, dep string) bool {
	matches, _ := filepath.Glob(filepath.Join(treeRoot, "*", dep, "Portfile"))
	if len(matches) != 1 {
		return false
	}
	b, err := os.ReadFile(matches[0])
	return err == nil && bytes.Contains(b, []byte("nomaintainer"))
}

// describeUnverifiedTip says what an unnoted tip means. The finding is
// verdict's; the reading is this function's — the records the notes ref
// holds, and the records on the branch's own history with their
// distance from the tip. Both sequences keep git's order, because the
// judgment names the first match in each and sorting them would
// quietly change which record it names. An unreadable note is stepped
// over here rather than reported: a drift sentence is a courtesy, and
// one bad note in the ref must not cost the whole line.
//
// The records are yielded one at a time, and the branch's history is
// not walked at all unless the notes answered nothing. Every element is
// a `git notes show`, so a tip whose content some record already covers
// — the amend this function mostly exists for — costs the reads up to
// that record and no rev-list.
func (e *Engine) describeUnverifiedTip(ctx context.Context, repo *git.Repo, branch, tip string) (string, error) {
	tipTree, err := repo.RevParse(ctx, tip+"^{tree}")
	if err != nil {
		return "", err
	}
	l := e.Ledger(repo)
	shas, err := l.All(ctx)
	if err != nil {
		return "", err
	}
	noted := func(yield func(verdict.Noted) bool) {
		for _, sha := range shas {
			n, err := l.Read(ctx, sha)
			if err != nil {
				continue
			}
			if !yield(verdict.Noted{Sha: git.Abbrev(sha), Record: n}) {
				return
			}
		}
	}
	if s := verdict.DriftOverTree(tipTree, noted); s != "" {
		return s, nil
	}
	ancestry, err := repo.RevList(ctx, branch, 32)
	if err != nil {
		return "", err
	}
	behind := func(yield func(verdict.Ancestor) bool) {
		for distance, sha := range ancestry {
			if distance == 0 {
				continue // the tip itself, which is the commit with no note
			}
			n, err := l.Read(ctx, sha)
			if err != nil {
				continue
			}
			if !yield(verdict.Ancestor{
				Noted:  verdict.Noted{Sha: git.Abbrev(sha), Record: n},
				Behind: distance,
			}) {
				return
			}
		}
	}
	return verdict.DriftBehind(branch, behind), nil
}

// LatestNote is the branch's most recent verification record: the
// tip's note, or the nearest one behind it.
func (e *Engine) LatestNote(ctx context.Context, repo *git.Repo, branch string) (record.Record, error) {
	shas, err := repo.RevList(ctx, branch, 32)
	if err != nil {
		return record.Record{}, err
	}
	for _, sha := range shas {
		if n, err := e.Ledger(repo).Read(ctx, sha); err == nil {
			return n, nil
		}
	}
	return record.Record{}, git.ErrNoNote
}
