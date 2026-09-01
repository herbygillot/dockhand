package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
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
func InspectBranch(ctx context.Context, rs *runstate.Context, repo *git.Repo, branch string) (string, *Note, string, error) {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return "", nil, "", err
	}
	n, err := ReadNote(ctx, repo, tip)
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
	if n.AnyState("running") {
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
func SettleRuns(ctx context.Context, rs *runstate.Context, repo *git.Repo, n *Note) error {
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
	if fresh, rerr := ReadNote(ctx, repo, n.Sha); rerr == nil {
		*n = fresh
	}
	changed := false
	for plat, r := range n.Runs {
		if r.State != "running" {
			continue
		}
		st, perr := prov.Poll(ctx, r.Job)
		if errors.Is(perr, verify.ErrUnknownJob) {
			r.State, r.Detail = "errored", "job vanished: its worker no longer exists"
			n.Runs[plat], changed = r, true
			continue
		}
		if perr != nil {
			return perr
		}
		switch st.State {
		case verify.Running:
			continue
		case verify.Passed:
			r.State = "passed"
			if r.Linted {
				// The log is about to become unreachable — a passing
				// run's worker is released — so what lint said is read
				// now or never. This is the lint box's corroboration.
				if log, lerr := prov.Log(ctx, r.Job); lerr == nil {
					r.Lint = LintSummary(log)
				}
			}
			if rerr := prov.Release(ctx, r.Job); rerr != nil {
				r.Detail = "worker not released: " + rerr.Error()
			}
		case verify.Failed:
			r.State, r.Handle = "failed", st.Handle
			if log, lerr := prov.Log(ctx, r.Job); lerr == nil {
				if portDeclined(log) {
					r.State, r.Handle = "unsupported", ""
					r.Detail = "the port declines to build on this platform"
					_ = prov.Release(context.WithoutCancel(ctx), r.Job)
				} else {
					// The diagnosis rides the note, so status answers
					// "why" without a log dig — the failure-side twin
					// of the lint evidence.
					r.Detail = failureSummary(log)
					// A failure that names a DIFFERENT port is a
					// dependency breaking before the change was ever
					// reached: the branch is untested, not disproven.
					// blocked, not failed — and the worker is released,
					// because the breakage belongs to a port this
					// branch never touched (field-measured on gomuks,
					// whose verdict blamed the bump for olm).
					if dep, ok := dependencyFailure(r.Detail, n.Port); ok {
						r.State, r.Handle = "blocked", ""
						r.Detail = blockedDetail(repo.Root, dep)
						_ = prov.Release(context.WithoutCancel(ctx), r.Job)
					}
				}
			}
		case verify.Errored:
			r.State, r.Detail = "errored", st.Detail
			_ = prov.Release(context.WithoutCancel(ctx), r.Job)
		}
		n.Runs[plat], changed = r, true
	}
	if !changed {
		return nil
	}
	return WriteNote(ctx, repo, *n)
}

// RenderNote is the human rendering of a verdict set: one line per
// platform, in stable order.
func RenderNote(n Note) []string {
	var lines []string
	for _, plat := range n.Platforms() {
		r := n.Runs[plat]
		s := r.State
		if r.State == "running" {
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

// SummarizeNote compresses a verdict set to one clause, for the
// drift lines.
func SummarizeNote(n Note) string {
	var parts []string
	for _, plat := range n.Platforms() {
		parts = append(parts, n.Runs[plat].State+" ("+plat+")")
	}
	return strings.Join(parts, ", ")
}

// lintRE matches port lint's own summary line.
var lintRE = regexp.MustCompile(`(\d+) errors? and (\d+) warnings? found`)

// lintSummary compresses a run's lint outcome to what a reviewer
// wants: "clean", or the warning count — the run already failed if
// there were errors. Empty when the log carries no lint summary.
func LintSummary(log string) string {
	m := lintRE.FindStringSubmatch(log)
	switch {
	case m == nil:
		return ""
	case m[2] == "0":
		return "clean"
	case m[2] == "1":
		return "1 warning"
	}
	return m[2] + " warnings"
}

// failedPortRE reads which port a MacPorts failure line blames — the
// "Failed to <phase> <name>:" shape every phase failure opens with.
var failedPortRE = regexp.MustCompile(`^Failed to [a-z]+ ([A-Za-z0-9._+-]+):`)

// dependencyFailure reports the port a failure summary blames when it
// is not the port under test. Conservative like portDeclined: a line
// that names no port, or names the port itself, changes nothing.
func dependencyFailure(summary, port string) (string, bool) {
	m := failedPortRE.FindStringSubmatch(summary)
	if m == nil || m[1] == port {
		return "", false
	}
	return m[1], true
}

// blockedDetail names the dependency that blocked a verification, and
// whether anyone maintains it — a nomaintainer dependency means there
// is no one to nudge, which changes what the maintainer does next.
// The lookup is best-effort against the tree; a port that cannot be
// found is simply not annotated.
func blockedDetail(treeRoot, dep string) string {
	who := ""
	if matches, _ := filepath.Glob(filepath.Join(treeRoot, "*", dep, "Portfile")); len(matches) == 1 {
		if b, err := os.ReadFile(matches[0]); err == nil && bytes.Contains(b, []byte("nomaintainer")) {
			who = " (nomaintainer)"
		}
	}
	return fmt.Sprintf("dependency %s%s fails to build; the change itself is untested", dep, who)
}

// failureSummary is the first substantive Error line of a failed run's
// log — the line naming which phase failed and why — skipping the
// boilerplate pointers that follow it. Empty when the log carries none.
func failureSummary(log string) string {
	for line := range strings.Lines(log) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Error: ") ||
			strings.HasPrefix(line, "Error: See ") ||
			strings.HasPrefix(line, "Error: Follow ") {
			continue
		}
		if len(line) > 160 {
			line = line[:160] + "…"
		}
		return strings.TrimPrefix(line, "Error: ")
	}
	return ""
}

// portDeclined reads a failure log for the shapes of a port refusing a
// platform rather than breaking on it. Conservative on purpose: an
// unrecognized refusal stays "failed", which is only ever a log-read
// away from the truth.
func portDeclined(log string) bool {
	for _, marker := range []string{"known to fail", "known_fail"} {
		if strings.Contains(log, marker) {
			return true
		}
	}
	return false
}

// describeUnverifiedTip says what an unnoted tip means: never
// verified, or verified at an older commit the branch has since moved
// past — the sha gap that IS the drift mechanism. Content identity is
// checked against every verdict, not just ancestors: an amend replaces
// the commit, so a reworded tip's verdicts live on a sha the branch no
// longer reaches, and the tree is what still matches.
func describeUnverifiedTip(ctx context.Context, repo *git.Repo, branch, tip string) (string, error) {
	tipTree, err := repo.RevParse(ctx, tip+"^{tree}")
	if err != nil {
		return "", err
	}
	noted, err := repo.NotesList(ctx, git.VerifyNotesRef)
	if err != nil {
		return "", err
	}
	for _, sha := range noted {
		n, err := ReadNote(ctx, repo, sha)
		if err != nil || n.Tree != tipTree || !n.AnyState("passed") {
			continue
		}
		return fmt.Sprintf("%s at %s — the tip differs only in commit metadata", SummarizeNote(n), sha[:12]), nil
	}
	shas, err := repo.RevList(ctx, branch, 32)
	if err != nil {
		return "", err
	}
	for behind, sha := range shas {
		if behind == 0 {
			continue
		}
		n, err := ReadNote(ctx, repo, sha)
		if err != nil {
			continue
		}
		return fmt.Sprintf("tip unverified; %s at %s, %d commit(s) behind — `dockhand verify %s` tests the tip",
			SummarizeNote(n), sha[:12], behind, branch), nil
	}
	return "unverified", nil
}

// OrphanWorkers lists them: running workers no note accounts for.
// Orphan is a running worker no note here accounts for, with the
// owning checkout when the attribution sidecar knows it.
type Orphan struct {
	Name  string `json:"name"`
	Owner string `json:"owner,omitempty"`
}

func OrphanWorkers(ctx context.Context, repo *git.Repo) []Orphan {
	if !TartPresent() {
		return nil
	}
	out, err := tart.CLI(ctx, nil, "list", "--quiet")
	if err != nil {
		return nil
	}
	tracked := map[string]bool{}
	if noted, err := repo.NotesList(ctx, git.VerifyNotesRef); err == nil {
		for _, sha := range noted {
			if n, err := ReadNote(ctx, repo, sha); err == nil {
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
func LatestNote(ctx context.Context, repo *git.Repo, branch string) (Note, error) {
	shas, err := repo.RevList(ctx, branch, 32)
	if err != nil {
		return Note{}, err
	}
	for _, sha := range shas {
		if n, err := ReadNote(ctx, repo, sha); err == nil {
			return n, nil
		}
	}
	return Note{}, git.ErrNoNote
}
