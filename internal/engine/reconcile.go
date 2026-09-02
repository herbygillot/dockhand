package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// ReconcileOpts shapes one pass over the dockhand/* namespace.
//
// The zero value is the sweep's opposite: observe every branch, judge
// every pull request, retire what merged, start nothing. Each field
// below turns one phase off or on, and the two verbs that ask for a
// pass differ only in these three bits — which is the point of having
// one reconciler rather than two traversals that were supposed to agree.
type ReconcileOpts struct {
	// RetireOnly is `clean`'s shape: judge and retire without observing
	// verification standings at all. The sweep asks one question of each
	// branch — did its pull request merge — and polling a worker to
	// answer it would be work whose answer nobody reads.
	RetireOnly bool

	// NoClean withholds the deletion without changing the verdict: what
	// a merged PR means does not depend on being asked to act on it.
	NoClean bool

	// Drain starts what was deferred, once every branch has been
	// observed and judged. Strictly last, and strictly after the
	// standings are taken: a run this pass starts must be reported as
	// the deferred run it was when the pass began, not as the verifying
	// run the pass just made it.
	Drain bool
}

// Reconcile is the read side: one pass over dockhand/* that observes
// every branch, judges what it found, applies the judgments, retires
// what merged, starts what was deferred, and hands back the whole of it
// for rendering.
//
// The phases are ordered by what each one may see. Observation happens
// outside the notes lock, judgment is pure, application compares before
// it writes, and only then does anything get deleted or started. Retire
// precedes drain deliberately and not alphabetically: with the drain
// first, a branch whose PR merged and whose run was deferred would have
// its run submitted — booting a guest, taking a slot — and then be
// deleted, releasing the worker the same pass had just started.
//
// Nothing here prints. What the pass says while acting comes back on
// the report, because `status`, `status --json` and `clean` put those
// words in three different places and only the renderer knows which.
func (e *Engine) Reconcile(ctx context.Context, o ReconcileOpts) (render.Report, error) {
	repo, err := e.Repo(ctx)
	if err != nil {
		return render.Report{}, err
	}
	branches, err := repo.Branches(ctx, git.BranchNamespace)
	if err != nil {
		return render.Report{}, err
	}
	// One clock read for the pass. Every running run's elapsed time is
	// measured from it, so two branches in one report cannot disagree
	// about what time it is.
	rep := render.Report{Repository: repo.Root, Now: time.Now()}
	if len(branches) == 0 {
		return rep, nil
	}
	f := &forge{repo: repo}
	if o.RetireOnly {
		// The sweep resolves the forge's coordinates up front and fails
		// on them: it has nothing to sweep by without them, and saying so
		// once beats saying it once per branch. The report resolves them
		// lazily instead — see forge.
		if err := f.load(ctx); err != nil {
			return render.Report{}, err
		}
	}
	for _, br := range branches {
		b := render.BranchReport{Branch: br}
		if !o.RetireOnly {
			tip, n, drift, ierr := e.inspect(ctx, repo, br)
			if ierr != nil {
				// Reported, never returned: one unreadable branch must not
				// cost the report of every other one.
				b.ObserveErr = ierr.Error()
			} else {
				b.Tip, b.Note, b.Drift = tip, n, drift
			}
		}
		e.retire(ctx, repo, f, &b, o, rep.Now)
		rep.Branches = append(rep.Branches, b)
	}
	if o.Drain {
		// The drain announces what it started, and its words go into the
		// report behind the branches rather than onto a stream here, so
		// that the machine rendering can route them to stderr and so that
		// two phases' prose stays in the order the pass produced it.
		said := &proseLines{stream: render.ToErr}
		drainer := *e
		drainer.Err = said
		drainer.PumpDeferred(ctx, repo, branches)
		rep.Drain = said.lines
	}
	return rep, nil
}

// retire judges one branch's pull request and acts on the merged
// verdict — the one deletion a report performs, because a merged PR is
// GitHub's own word that the work landed. Everything else stays
// reporting: open PRs, closed ones, and unpromoted branches say their
// state and stand.
//
// The two verbs part company only in where a failure goes. The report
// states an unanswerable lookup as the branch's PR standing and keeps
// its verification standing above it; the sweep has nothing else to say
// about that branch, so the failure is the line.
func (e *Engine) retire(ctx context.Context, repo *git.Repo, f *forge, b *render.BranchReport, o ReconcileOpts, now time.Time) {
	if repo.TrackedRemote(ctx, b.Branch) == "" {
		// Never pushed: there is no pull request to ask about, and no
		// reason to spend a gh call finding that out.
		return
	}
	b.Retire.Promoted = true
	fail := func(err error) {
		if o.RetireOnly {
			b.SweepErr = err.Error()
			return
		}
		b.Retire.Err = err.Error()
	}
	if err := f.load(ctx); err != nil {
		fail(err)
		return
	}
	pr, found, err := gh.LookupPR(ctx, e.Gh, repo, f.remotes, f.upstream, b.Branch)
	if err != nil {
		fail(err)
		return
	}
	if !found {
		return
	}
	b.Retire.PR, b.PR = PRFact(pr, true), pr
	d := verdict.DecideRetire(true, b.Retire.PR)
	// The audit is closed from the verdict rather than from the
	// demolition, and before it. From the verdict, because a merged pull
	// request is the change's outcome whether or not the deletion was
	// asked for, and because a rejection retires nothing at all yet is
	// exactly the outcome worth counting. Before it, because the row is
	// keyed by the tip, and the demolition is about to take away the
	// branch that tip is reached through.
	e.settleOutcome(ctx, repo, b, d, pr.MergeSha, now)
	if o.RetireOnly {
		if d != verdict.RetireMerged {
			return
		}
		// Only the merged verdict pays for the byte comparison: it is
		// several git calls per branch, and on any other verdict its
		// answer goes unread while its failure would turn a clean report
		// into an error. It is read before the demolition because the
		// demolition removes the branch it compares.
		landed, cerr := ContentLanded(ctx, repo, b.Branch)
		if cerr != nil {
			fail(cerr)
			return
		}
		said, derr := e.Discard(ctx, repo, b.Branch, true)
		b.Prose = append(b.Prose, said...)
		if derr != nil {
			fail(derr)
			return
		}
		b.Landed, b.Retire.Cleaned = landed, true
		return
	}
	if !d.Cleans(o.NoClean) {
		return
	}
	said, derr := e.Discard(ctx, repo, b.Branch, true)
	b.Prose = append(b.Prose, said...)
	if derr != nil {
		// The report reached the verdict and could not carry it out,
		// which is a different sentence from not having reached one.
		b.Retire.CleanErr = derr.Error()
		return
	}
	b.Retire.Cleaned = true
}

// settleOutcome closes this branch's audit row with what the forge said
// became of it. Only the two verdicts that are outcomes reach the log: a
// merge and a rejection are what a published change ends as, while an
// open pull request, a missing one and an unpromoted branch are all
// still in progress and have nothing to close.
//
// A branch this dockhand never published carries no rows and settles
// nothing, which is why the sha is asked for only after the verdict
// says a row could exist. The sweep observes no standings, so it has no
// tip in hand and pays one rev-parse for it here — on the merged and
// rejected branches only, which is the handful the pass was going to act
// on anyway.
//
// Failure is a warning and never the branch's line. What became of a
// pull request is what the reader asked about; that the bookkeeping
// about it could not be written is worth saying, and worth saying
// somewhere it cannot displace the answer.
func (e *Engine) settleOutcome(ctx context.Context, repo *git.Repo, b *render.BranchReport, d verdict.Retirement, mergeSha string, now time.Time) {
	var outcome record.Outcome
	switch d {
	case verdict.RetireMerged:
		outcome = record.Merged
	case verdict.RetireClosed:
		outcome = record.Rejected
	case verdict.RetireUnpromoted, verdict.RetireNoPR, verdict.RetireOpen:
		return
	}
	sha := b.Tip
	if sha == "" {
		var err error
		if sha, err = repo.RevParse(ctx, b.Branch); err != nil {
			b.Prose = append(b.Prose, render.Line{Stream: render.ToErr,
				Text: fmt.Sprintf("warning: recording the outcome of %s: %v", b.Branch, err)})
			return
		}
	}
	if err := e.Ledger(repo).Settle(ctx, sha, outcome, mergeSha, stamp(now)); err != nil {
		b.Prose = append(b.Prose, render.Line{Stream: render.ToErr,
			Text: fmt.Sprintf("warning: recording the outcome of %s: %v", b.Branch, err)})
	}
}

// PRFact maps gh's answer about a pull request into the fact a judgment
// weighs. GitHub's own spellings — a merge timestamp being present, a
// state word reading "open" — are read here, at the boundary where its
// JSON is already being read, so that every judgment reaches the same
// shape and no decision has to know what gh prints.
//
// A lookup that found nothing is the zero fact, which is what "no pull
// request" means.
func PRFact(pr gh.PullRequest, found bool) verdict.PRFact {
	if !found {
		return verdict.PRFact{}
	}
	return verdict.PRFact{
		Found:  true,
		Number: pr.Number,
		Title:  pr.Title,
		URL:    pr.HTMLURL,
		Merged: pr.MergedAt != "",
		Open:   pr.State == "open",
	}
}

// forge is what looking a pull request up needs besides the branch: the
// upstream repository and the remote table, resolved once for the pass.
//
// Lazily, because a namespace where nothing was ever promoted must cost
// no gh call at all — that is the common shape on a machine with no
// network and a pocket of local work, and paying for a lookup nobody
// will use is how a report becomes something to avoid running. The
// sweep loads it eagerly for the opposite reason, in Reconcile.
type forge struct {
	repo     *git.Repo
	upstream string
	remotes  map[string]string
	err      error
	loaded   bool
}

func (f *forge) load(ctx context.Context) error {
	if f.loaded {
		return f.err
	}
	f.loaded = true
	f.upstream, f.err = gh.UpstreamRepo(ctx, f.repo)
	if f.err == nil {
		f.remotes, f.err = f.repo.Remotes(ctx)
	}
	return f.err
}

// proseLines stands in for a stream so that a phase which prints as it
// works can be carried into the report instead. Every writer it
// replaces prints one whole line per call, which is what makes
// splitting on the trailing newline exact rather than a guess.
type proseLines struct {
	stream render.Stream
	lines  []render.Line
}

func (p *proseLines) Write(b []byte) (int, error) {
	for _, s := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		p.lines = append(p.lines, render.Line{Stream: p.stream, Text: s})
	}
	return len(b), nil
}
