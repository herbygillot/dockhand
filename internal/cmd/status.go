package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
)

// statusAction reconciles the dockhand/* namespace: every branch, its
// tip's verification record, and the drift between them. It is a
// reconciler, not a daemon: running jobs are polled here, their
// verdicts written back to the notes, workers released on pass, and —
// the one deletion status performs — a branch whose PR merged is
// cleaned, announced, because a merged PR is GitHub's own word that
// the work landed. Every other cleanup is the user's explicit act.
type statusAction struct {
	json    bool
	noClean bool
}

var _ Action = statusAction{}

func (a statusAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	branches, err := repo.Branches(ctx, "dockhand/")
	if err != nil {
		return err
	}
	if a.json {
		return statusAsJSON(ctx, rs, repo, branches, a.noClean)
	}
	if len(branches) == 0 {
		// Naming the repository is the point: run from the wrong
		// checkout, "no branches" is true and useless — true and
		// located is actionable.
		fmt.Fprintf(rs.Out, "no dockhand branches in %s\n", repo.Root)
		reportOrphanWorkers(ctx, rs, repo)
		return nil
	}
	pr := newPRStatus(ctx, repo, a.noClean)
	for _, br := range branches {
		lines, err := lifecycle.DescribeBranch(ctx, rs, repo, br)
		if err != nil {
			lines = []string{"error: " + err.Error()}
		}
		if cleaned, extra := pr.reconcile(ctx, rs, br); cleaned {
			lines = []string{extra}
		} else if extra != "" {
			lines = append(lines, extra)
		}
		if len(lines) == 1 {
			fmt.Fprintf(rs.Out, "%-32s %s\n", br, lines[0])
			continue
		}
		fmt.Fprintln(rs.Out, br)
		for _, l := range lines {
			fmt.Fprintf(rs.Out, "  %s\n", l)
		}
	}
	pumpDeferred(ctx, rs, repo, branches)
	reportOrphanWorkers(ctx, rs, repo)
	return nil
}

// pumpDeferred starts what was deferred, now that this status pass
// has settled finished runs and freed their slots. Every deferred run
// gets one attempt, whatever its recorded reason — conditions change
// (a base provisioned, a slot freed), and the attempt re-records the
// truth either way. The one early exit is a typed capacity refusal:
// the machine is full, so further attempts this pass are noise. This
// is the reconciler acting, not a daemon — a field batch run sat
// eight deferred branches against an idle machine because the old
// message promised a pump that did not exist.
func pumpDeferred(ctx context.Context, rs *runstate.Context, repo *git.Repo, branches []string) {
	if !lifecycle.TartPresent() {
		return
	}
	for _, br := range branches {
		tip, err := repo.RevParse(ctx, br)
		if err != nil {
			continue // cleaned mid-pass, or never a branch
		}
		n, err := lifecycle.ReadNote(ctx, repo, tip)
		if err != nil {
			continue
		}
		for _, plat := range n.Platforms() {
			if n.Runs[plat].State != "deferred" {
				continue
			}
			rel, derr := branchPortdir(ctx, repo, br, tip)
			if derr != nil {
				fmt.Fprintf(rs.Err, "%s: deferred %s not retried: %v\n", br, plat, derr)
				continue
			}
			release, ok := platformNamed(ctx, rs, plat)
			if !ok {
				fmt.Fprintf(rs.Err, "%s: deferred %s not retried: no such platform is provisioned\n", br, plat)
				continue
			}
			if pumpRun(ctx, rs, repo, br, tip, rel, plat, release) {
				return
			}
		}
	}
}

// pumpRun retries one deferred run and reports whether the pass should
// stop here. The retry is a claim as much as a submit: two status
// passes sharing a checkout — two agents, which is how the tool is now
// used — both read the run as deferred, both submitted, and the second
// RecordRun overwrote the first's job, leaving a worker no note
// accounted for. Schema 2 has no field to claim a run with (a peer
// binary's WriteNote round-trips the struct and drops what it does not
// know), so the claim is a lock held from the re-read through the
// record: the holder re-reads the note, and a run no longer deferred
// was started or settled by the other claimant — skipped, silently,
// because that claimant announced it.
func pumpRun(ctx context.Context, rs *runstate.Context, repo *git.Repo, br, tip, rel, plat string, release platform.Release) (stop bool) {
	// Lock order, checked against every holder at HEAD 82f2f2c. The
	// submit lock has two takers, this pump and verifyBranch, and
	// neither takes it under any other lock. Inside it,
	// SubmitVerification takes tart's admission lock (Provider.Submit,
	// released once the guest is visibly running, before stage and
	// launch) and then the notes flock (RecordRun, released before the
	// compensating Release runs) — in sequence, never nested in each
	// other. No holder of either inner lock reaches back for this one,
	// and neither inner lock is taken under the other: the admission
	// holders (Provider.Submit, RunOnBase, provision's boot) never
	// touch notes, and the notes holders (SettleRuns, RecordRun,
	// CancelStale, CancelRunning, DiscardBranch, cancel) call only
	// Provider.Release, which is `tart stop` and `tart delete` and
	// takes no admission. submit → admission and submit → notes are
	// the only edges; there is no cycle. Why it is a lock of its own
	// and not the notes lock is on git.(*Repo).LockSubmit.
	unlock, err := repo.LockSubmit(ctx, submitLockWait)
	if errors.Is(err, lockfile.ErrHeld) {
		// The expected contention: a peer mid-submit holds it past the
		// wait, and would hold it for every run after this one too.
		// The pass stops, and the peer's own status names what it
		// started. Not the lock's own text, which sends the user
		// hunting a hung process that is booting a guest on purpose.
		fmt.Fprintf(rs.Err, "%s: deferred %s not retried: another dockhand is starting deferred runs in this repository; its status names what it started\n", br, plat)
		return true
	}
	if err != nil {
		fmt.Fprintf(rs.Err, "%s: deferred %s not retried: %v\n", br, plat, err)
		return true
	}
	defer unlock()
	n, err := lifecycle.ReadNote(ctx, repo, tip)
	if err != nil {
		// No note is a branch discarded under this pass: nothing left
		// to start. Anything else — a peer's newer schema, a corrupt
		// note, git failing — is a run this pass could not judge, and
		// the second read exists to notice a peer's writes, so it says.
		if !errors.Is(err, git.ErrNoNote) {
			fmt.Fprintf(rs.Err, "%s: deferred %s not retried: %v\n", br, plat, err)
		}
		return false
	}
	run := n.Runs[plat]
	if run.State != "deferred" {
		return false
	}
	// The note names what this branch verifies — for a minted
	// branch, the SUBPORT the plan bumped. The portdir's base
	// name is the parent port, and submitting that would build
	// the untouched main port and call the branch verified
	// (field-caught on pcre2, whose portdir is devel/pcre).
	portName := n.Port
	if portName == "" {
		portName = filepath.Base(rel)
	}
	err = lifecycle.SubmitVerification(ctx, rs, &lifecycle.Minted{
		Repo: repo, Branch: br, Sha: tip, RelPort: rel,
	}, portName, release, false, run.Tested)
	var vde *lifecycle.VerifyDeferredError
	if errors.As(err, &vde) {
		if rerr := lifecycle.RecordRun(ctx, rs, repo, tip, portName, plat, lifecycle.Run{
			State: "deferred", Detail: vde.Reason, Tested: run.Tested,
		}, ""); rerr != nil {
			fmt.Fprintf(rs.Err, "warning: re-recording deferred run: %v\n", rerr)
		}
		var cap_ *verify.CapacityError
		if errors.As(err, &cap_) {
			fmt.Fprintf(rs.Err, "still waiting for a slot: %s on %s (and anything deferred after it)\n", br, plat)
			return true
		}
		fmt.Fprintf(rs.Err, "still deferred: %s on %s — %s\n", br, plat, vde.Reason)
		return false
	}
	if err != nil {
		fmt.Fprintf(rs.Err, "%s: deferred %s not retried: %v\n", br, plat, err)
	}
	return false
}

// submitLockWait bounds a claimant's wait for a peer's submit — the
// pump's, and verify's. A submit that never boots — a capacity refusal,
// a test double — is over in a couple of seconds, and waiting it out
// lets the re-read find the run started and skip cleanly; a submit
// that boots a guest holds the lock for minutes, which no claimant
// should sit through: the peer is starting the very run this one
// would have. A variable so the contention test need not wait it out;
// the tests in internal/cmd are serial by design (none calls
// t.Parallel), which is what makes assigning it from a test safe.
var submitLockWait = 5 * time.Second

// platformNamed resolves a run's recorded platform key against the
// provider's provisioned platforms.
func platformNamed(ctx context.Context, rs *runstate.Context, name string) (platform.Release, bool) {
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return platform.Release{}, false
	}
	for _, r := range prov.Capabilities().Platforms {
		if r.Name == name {
			return r, true
		}
	}
	return platform.Release{}, false
}

// statusJSON is the machine rendering of the same reconciliation the
// human one performs — the polling, settling, and merged-PR autoclean
// all still happen; only the telling differs. The note is emitted as
// stored (its own JSON is the schema), so a consumer reads exactly
// what a future dockhand would.
type statusJSON struct {
	Repository    string             `json:"repository"`
	Branches      []statusBranch     `json:"branches"`
	OrphanWorkers []lifecycle.Orphan `json:"orphan_workers,omitempty"`
}

type statusBranch struct {
	Branch string `json:"branch"`
	Tip    string `json:"tip,omitempty"`
	// Note is the tip's verdict set, absent when the tip has none.
	Note *lifecycle.Note `json:"note,omitempty"`
	// Drift is the human sentence about an unnoted tip — content
	// identity, commits behind — kept as prose: it is a finding, not a
	// state machine.
	Drift   string             `json:"drift,omitempty"`
	PR      *forge.PullRequest `json:"pr,omitempty"`
	PRError string             `json:"pr_error,omitempty"`
	Cleaned bool               `json:"cleaned,omitempty"`
	Error   string             `json:"error,omitempty"`
}

func statusAsJSON(ctx context.Context, rs *runstate.Context, repo *git.Repo, branches []string, noClean bool) error {
	out := statusJSON{Repository: repo.Root, Branches: []statusBranch{}}
	// Stdout is the document, so every side-effect's prose — an
	// autoclean announcing itself, a discard's fork warning — routes to
	// stderr. Field-measured: a merged-PR clean mid---json wrote
	// "discarded …" into the JSON and broke the consumer.
	prose := *rs
	prose.Out = rs.Err
	pr := newPRStatus(ctx, repo, noClean)
	for _, br := range branches {
		b := statusBranch{Branch: br}
		tip, n, drift, err := lifecycle.InspectBranch(ctx, rs, repo, br)
		if err != nil {
			b.Error = err.Error()
		} else {
			b.Tip, b.Note, b.Drift = tip, n, drift
		}
		outcome := pr.judge(ctx, &prose, br)
		b.Cleaned = outcome.cleaned
		b.PRError = outcome.errText
		if outcome.found {
			prCopy := outcome.pr
			b.PR = &prCopy
		}
		out.Branches = append(out.Branches, b)
	}
	pumpDeferred(ctx, &prose, repo, branches)
	if workers := lifecycle.OrphanWorkers(ctx, repo); len(workers) > 0 {
		out.OrphanWorkers = workers
	}
	enc := json.NewEncoder(rs.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// prStatus carries the lazily-fetched pieces the PR half of status
// needs: the upstream repo and the remote table, resolved once.
type prStatus struct {
	noClean  bool
	repo     *git.Repo
	upstream string
	remotes  map[string]string
	broken   error
	loaded   bool
}

func newPRStatus(_ context.Context, repo *git.Repo, noClean bool) *prStatus {
	return &prStatus{repo: repo, noClean: noClean}
}

// prOutcome is one branch's judged PR standing, structured for both
// renderings.
type prOutcome struct {
	promoted bool
	found    bool
	cleaned  bool
	pr       forge.PullRequest
	errText  string
	cleanErr string
}

// judge reports a promoted branch's PR standing — and when the PR
// merged, the branch is done: status cleans it, the one deletion
// status performs, because a merged PR is GitHub's own word that the
// work landed. Everything else stays reporting: open PRs, closed
// ones, and unpromoted branches say their state and stand.
func (ps *prStatus) judge(ctx context.Context, rs *runstate.Context, branch string) prOutcome {
	if ps.repo.TrackedRemote(ctx, branch) == "" {
		return prOutcome{}
	}
	if !ps.loaded {
		ps.loaded = true
		ps.upstream, ps.broken = forge.UpstreamRepo(ctx, ps.repo)
		if ps.broken == nil {
			ps.remotes, ps.broken = ps.repo.Remotes(ctx)
		}
	}
	if ps.broken != nil {
		return prOutcome{promoted: true, errText: ps.broken.Error()}
	}
	pr, found, err := forge.LookupPR(ctx, rs.RunGH, ps.repo, ps.remotes, ps.upstream, branch)
	if err != nil {
		return prOutcome{promoted: true, errText: err.Error()}
	}
	if !found {
		return prOutcome{promoted: true}
	}
	o := prOutcome{promoted: true, found: true, pr: pr}
	if pr.MergedAt != "" && !ps.noClean {
		if err := lifecycle.DiscardBranch(ctx, rs, ps.repo, branch, true); err != nil {
			o.cleanErr = err.Error()
			return o
		}
		o.cleaned = true
	}
	return o
}

// reconcile is judge's human rendering: (cleaned, line).
func (ps *prStatus) reconcile(ctx context.Context, rs *runstate.Context, branch string) (cleaned bool, line string) {
	o := ps.judge(ctx, rs, branch)
	switch {
	case !o.promoted:
		return false, ""
	case o.errText != "":
		return false, "PR state unavailable: " + o.errText
	case !o.found:
		return false, "promoted; no PR found"
	case o.cleanErr != "":
		return false, fmt.Sprintf("PR #%d merged; cleaning failed: %s", o.pr.Number, o.cleanErr)
	case o.cleaned:
		return true, fmt.Sprintf("PR #%d merged — branch cleaned", o.pr.Number)
	case o.pr.MergedAt != "":
		// --no-clean: report the merge, withhold the deletion.
		return false, fmt.Sprintf("PR #%d merged — `dockhand clean` removes the branch", o.pr.Number)
	case o.pr.State == "open":
		return false, fmt.Sprintf("PR #%d open (%s)", o.pr.Number, o.pr.HTMLURL)
	default:
		return false, fmt.Sprintf("PR #%d closed without merging", o.pr.Number)
	}
}

// reportOrphanWorkers names running workers no note accounts for: a
// pre-mint gate failure keeps its environment with no branch, another
// checkout's jobs are invisible here, and with a two-guest cap a
// forgotten worker is an expensive kind of quiet. Best-effort — a
// machine without tart has no workers to report.
func reportOrphanWorkers(ctx context.Context, rs *runstate.Context, repo *git.Repo) {
	for _, o := range lifecycle.OrphanWorkers(ctx, repo) {
		if o.Owner != "" {
			fmt.Fprintf(rs.Out, "%-32s worker from %s — `dockhand status` there follows it\n", o.Name, o.Owner)
			continue
		}
		fmt.Fprintf(rs.Out, "%-32s untracked worker — `dockhand shell %s` reaches it; `tart delete %s` frees the slot\n", o.Name, o.Name, o.Name)
	}
}

// Status builds the status subcommand.
func Status() *cobra.Command {
	var asJSON, noClean bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Report every dockhand branch and its verification standing",
		Args:  noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return statusAction{json: asJSON, noClean: noClean}, nil
		}),
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON on stdout")
	c.Flags().BoolVar(&noClean, "no-clean", false, "report merged PRs without deleting their branches")
	return c
}
