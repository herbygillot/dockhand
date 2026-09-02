package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// DescribeBranch renders one branch's verification standing, polling
// and settling whatever is still running on its tip.
func DescribeBranch(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch string) ([]string, error) {
	_, n, drift, err := InspectBranch(ctx, rs, repo, branch)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return []string{drift}, nil
	}
	return RenderNote(*n), nil
}

// InspectBranch is the structured half DescribeBranch and the JSON
// rendering share: the tip, its settled note (nil when unnoted), and
// the drift finding for an unnoted tip.
func InspectBranch(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch string) (string, *record.Record, string, error) {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return "", nil, "", err
	}
	n, err := ledger.Open(repo).Read(ctx, tip)
	if errors.Is(err, git.ErrNoNote) {
		drift, derr := describeUnverifiedTip(ctx, repo, branch, tip)
		if derr != nil {
			return tip, nil, "", derr
		}
		return tip, nil, drift, nil
	}
	if err != nil {
		return tip, nil, "", err
	}
	if n.AnyState(record.Running) {
		if err := SettleRuns(ctx, rs, repo, &n); err != nil {
			return tip, nil, "", err
		}
	}
	return tip, &n, "", nil
}

// SettleRuns polls every running run and writes what it learns back to
// the note. Poll never mutates and Release is the caller's: status
// releases the worker on pass — a kept green environment is a wasted
// slot — and keeps it on failure, where it is the debug handle. A
// failure whose log shows the port refusing the platform records as
// unsupported instead, and its worker is released: a correct refusal
// leaves nothing to debug.
func SettleRuns(ctx context.Context, rs *runstate.Context, repo *git.Repo, n *record.Record) error {
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return nil // running, cannot poll; the note stands as is
	}
	// The critical section spans the whole read-modify-write, and the
	// note is RE-READ under the lock: the caller's copy may predate a
	// concurrent dockhand's record — two agents share this checkout
	// now — and settling a stale copy would write the lost update this
	// lock exists to prevent.
	unlock, err := repo.LockNotes(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	l := ledger.Open(repo)
	if fresh, rerr := l.Read(ctx, n.Sha); rerr == nil {
		*n = fresh
	}
	changed := false
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
		n.Runs[plat], changed = j.Run, true
	}
	if !changed {
		return nil
	}
	return l.Write(ctx, *n)
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

// RenderNote is the human rendering of a verdict set: one line per
// platform, in stable order.
func RenderNote(n record.Record) []string {
	var lines []string
	for _, plat := range n.Platforms() {
		r := n.Runs[plat]
		// The wire word is the line's own text until a running run
		// replaces it with its elapsed time.
		s := string(r.State)
		if r.State == record.Running {
			s = fmt.Sprintf("verifying (%s)", time.Since(r.Job.Started).Round(time.Second))
		}
		line := fmt.Sprintf("%s (%s)", s, plat)
		if r.Handle != "" {
			line += " — environment kept: " + r.Handle
		}
		if r.Detail != "" {
			line += " — " + r.Detail
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{"no runs recorded"}
	}
	return lines
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
func describeUnverifiedTip(ctx context.Context, repo *git.Repo, branch, tip string) (string, error) {
	tipTree, err := repo.RevParse(ctx, tip+"^{tree}")
	if err != nil {
		return "", err
	}
	l := ledger.Open(repo)
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

// OrphanWorkers lists them: running workers no note accounts for.
// Orphan is a running worker no note here accounts for, with the
// owning checkout when the attribution sidecar knows it.
type Orphan struct {
	Name  string `json:"name"`
	Owner string `json:"owner,omitempty"`
}

func OrphanWorkers(ctx context.Context, tools *tool.Finder, repo *git.Repo) []Orphan {
	if !TartPresent(tools) {
		return nil
	}
	out, err := tart.CLI(ctx, tools, nil, "list", "--quiet")
	if err != nil {
		return nil
	}
	tracked := map[string]bool{}
	l := ledger.Open(repo)
	if noted, err := l.All(ctx); err == nil {
		for _, sha := range noted {
			if n, err := l.Read(ctx, sha); err == nil {
				for _, r := range n.Runs {
					tracked[r.Job.ID] = true
					tracked[r.Handle] = true
				}
			}
		}
	}
	var orphans []Orphan
	for _, line := range strings.Split(out, "\n") {
		vm := strings.TrimSpace(line)
		if !strings.HasPrefix(vm, tart.WorkerPrefix) || tracked[vm] {
			continue
		}
		o := Orphan{Name: vm}
		// The attribution sidecar can name the checkout that started a
		// worker this repository's notes know nothing about — the
		// cross-repo half of the untracked-worker story.
		if owner := tart.OwnerOf(vm); owner != "" && owner != repo.Root {
			o.Owner = owner
		}
		orphans = append(orphans, o)
	}
	return orphans
}

// LatestNote is the branch's most recent verification record: the
// tip's note, or the nearest one behind it.
func LatestNote(ctx context.Context, repo *git.Repo, branch string) (record.Record, error) {
	shas, err := repo.RevList(ctx, branch, 32)
	if err != nil {
		return record.Record{}, err
	}
	for _, sha := range shas {
		if n, err := ledger.Open(repo).Read(ctx, sha); err == nil {
			return n, nil
		}
	}
	return record.Record{}, git.ErrNoNote
}
