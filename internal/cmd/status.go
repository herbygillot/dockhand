package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lifecycle"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// statusAction reconciles the dockhand/* namespace: every branch, its
// tip's verification record, and the drift between them. It is a
// reconciler, not a daemon: running jobs are polled here, their
// verdicts written back to the notes, workers released on pass, and —
// the one deletion status performs — a branch whose PR merged is
// cleaned, announced, because a merged PR is GitHub's own word that
// the work landed. Every other cleanup is the user's explicit act.
type statusAction struct {
	json bool
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
		return statusAsJSON(ctx, rs, repo, branches)
	}
	if len(branches) == 0 {
		// Naming the repository is the point: run from the wrong
		// checkout, "no branches" is true and useless — true and
		// located is actionable.
		fmt.Fprintf(rs.Out, "no dockhand branches in %s\n", repo.Root)
		reportOrphanWorkers(ctx, rs, repo)
		return nil
	}
	pr := newPRStatus(ctx, repo)
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
	reportOrphanWorkers(ctx, rs, repo)
	return nil
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

func statusAsJSON(ctx context.Context, rs *runstate.Context, repo *git.Repo, branches []string) error {
	out := statusJSON{Repository: repo.Root, Branches: []statusBranch{}}
	// Stdout is the document, so every side-effect's prose — an
	// autoclean announcing itself, a discard's fork warning — routes to
	// stderr. Field-measured: a merged-PR clean mid---json wrote
	// "discarded …" into the JSON and broke the consumer.
	prose := *rs
	prose.Out = rs.Err
	pr := newPRStatus(ctx, repo)
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
	repo     *git.Repo
	upstream string
	remotes  map[string]string
	broken   error
	loaded   bool
}

func newPRStatus(ctx context.Context, repo *git.Repo) *prStatus {
	return &prStatus{repo: repo}
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
	if pr.MergedAt != "" {
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
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Report every dockhand branch and its verification standing",
		Args:  noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return statusAction{json: asJSON}, nil
		}),
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON on stdout")
	return c
}
