package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// ReconcileOpts shapes one pass over the branches dockhand observes.
//
// The zero value is `status` (D27): observe every branch, settle what
// finished, judge every pull request, and act on none of it — no
// deletion, no submit, no publication. Each field below turns one act
// on, or turns the observation itself down to the ledger, and the
// verbs that ask for a pass differ only in these bits. That is the
// point of having one reconciler rather than two traversals that were
// supposed to agree: `status` and `cycle` reach one verdict per branch
// by one code path, and part company only in what they do about it.
type ReconcileOpts struct {
	// NoUpdate is `status --no-update`: the ledger as written, and
	// nothing else (D27, ruled 2026-09-05 with its implementation,
	// pending the maintainer). No worker is polled, no note is written,
	// no lock is taken, no forge is asked and no provider is composed.
	// What that costs the report is every fact that lives outside the
	// ledger: a running run reads as it was last recorded, a pushed
	// branch's pull request reads as not checked, and there is no
	// worker audit. The report says so once, at the top, so a missing
	// pull request line is not read as a missing pull request.
	//
	// It excludes the other bits. A pure read has nothing to retire,
	// drain or publish that it has observed, and Reconcile returns
	// after the observation rather than consulting them.
	NoUpdate bool

	// Retire is `cycle`'s deletion: a branch whose pull request merged
	// is demolished, locally and off the fork. Only a branch dockhand
	// minted — one under git.BranchNamespace — is ever deleted, whatever
	// its pull request did; a hand-made branch that carries a verify
	// note is observed and settled and left where it is (D27's
	// fold-in). Under `status` this is off, and the merged line names
	// `dockhand cycle` instead.
	Retire bool

	// KeepMerged is `cycle --keep-merged`: withholds the deletion Retire
	// would perform, without changing the verdict — what a merged PR
	// means does not depend on whether anybody is willing to act on it.
	// A separate bit rather than Retire's negation because the report
	// words the two apart: a branch `status` did not delete names the
	// verb, a branch `cycle` was told to keep says it was kept, and no
	// kept case may be silent.
	KeepMerged bool

	// Drain starts what was deferred, once every branch has been
	// observed and judged. Strictly last, and strictly after the
	// standings are taken: a run this pass starts must be reported as
	// the deferred run it was when the pass began, not as the verifying
	// run the pass just made it. `cycle`'s; `status` reports the queue
	// and names the verb.
	Drain bool

	// Reclaim is `cycle --reclaim-orphans`: the workers the backend is
	// running that no note here claims are released — this checkout's
	// own and the unattributed; another checkout's are named and left
	// to it (ReclaimOrphans says why). It runs after retire and before
	// the drain (ruled 2026-09-05 with D27's implementation, pending the
	// maintainer): after, because a retirement gives back the guests of
	// the branch it demolishes and those must not be met here as
	// orphans; before, because the slot a reclaim frees should be one
	// this pass's drain can spend rather than the next pass's. Off by
	// default and never a keep-flag — it destroys environments nobody
	// has characterised, so it is the plain flag that asks.
	Reclaim bool

	// Publish is the machine's publish road, and nil is the answer for
	// every verb a person types (D27, ruled 2026-09-05 with its
	// implementation, pending the maintainer): `status` reports, and a
	// person's `cycle` retires and drains, and neither may open a pull
	// request as a side effect of being run. The unattended pass —
	// `cycle --auto`, which is what `auto` was — hands one in and reads
	// it back; see PublishSlot, whose Results carry what the pass
	// should exit with.
	//
	// A pointer rather than a bool because the road has settings the
	// caller owns (the per-pass cap, the pacing) and answers the caller
	// needs; a bool would put the settings somewhere else and the answers
	// nowhere.
	Publish *PublishSlot
}

// Reconcile is one pass over every branch dockhand has something to
// say about: it observes each one, settles what finished, judges what
// it found, and — asked to — retires what merged, publishes what is
// ready and starts what was deferred, handing back the whole of it for
// rendering.
//
// Which branches: the dockhand/* namespace, and every other local
// branch whose tip carries a verify note (D27's fold-in). A verify on a
// working branch mints a note that a namespace walk could never show,
// and our own epilogue points at `dockhand status` to follow it; the
// ledger is keyed by commit, so the note was always findable and only
// the walk was hiding it. Deletion does not widen with the view:
// retire demolishes only what dockhand minted.
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
// the report, because `status --json` and the two Text renderings put
// those words in different places and only the renderer knows which.
func (e *Engine) Reconcile(ctx context.Context, o ReconcileOpts) (render.Report, error) {
	repo, err := e.Repo(ctx)
	if err != nil {
		return render.Report{}, err
	}
	branches, err := e.observedBranches(ctx, repo)
	if err != nil {
		return render.Report{}, err
	}
	// One clock read for the pass. Every running run's elapsed time is
	// measured from it, so two branches in one report cannot disagree
	// about what time it is.
	rep := render.Report{Repository: repo.Root, Now: time.Now(), AsRecorded: o.NoUpdate}
	if len(branches) == 0 {
		return rep, nil
	}
	// Which of them were ever pushed, resolved once for the pass. A local
	// ref listing and nothing more, so the pure read may take it: it is
	// the gate on the forge question below, and under --no-update it is
	// what lets the line say "promoted; PR not checked" rather than
	// nothing at all.
	pushed, err := e.pushedAmong(ctx, repo, branches)
	if err != nil {
		return render.Report{}, err
	}
	f := &forge{repo: repo, pushed: pushed}
	for _, br := range branches {
		b := render.BranchReport{Branch: br, Minted: minted(br)}
		tip, n, drift, ierr := e.inspect(ctx, repo, br, !o.NoUpdate)
		if ierr != nil {
			// Reported, never returned: one unreadable branch must not
			// cost the report of every other one.
			b.ObserveErr = ierr.Error()
		} else {
			b.Tip, b.Note, b.Drift = tip, n, drift
		}
		e.retire(ctx, repo, f, &b, o, rep.Now)
		rep.Branches = append(rep.Branches, b)
	}
	if o.NoUpdate {
		return rep, nil
	}
	if o.Publish != nil {
		// The publish slot: after retire, before drain. publishPass states
		// both halves of that ordering; the short version is that a branch
		// whose PR just merged is not a branch to publish, and that the
		// standings this slot judges must be the ones the pass observed
		// rather than the ones the drain is about to change.
		e.publishPass(ctx, repo, &rep, o.Publish)
	}
	if o.Reclaim {
		// Between the retirements and the drain: see ReconcileOpts.Reclaim
		// for both halves of that ordering.
		rep.Reclaimed = e.ReclaimOrphans(ctx, repo)
	}
	if o.Drain {
		// The drain announces what it started, and its words go into the
		// report behind the branches rather than onto a stream here, so
		// that the machine rendering can route them to stderr and so that
		// two phases' prose stays in the order the pass produced it. It
		// walks the same branches the pass observed, hand-made ones
		// included: a queued run on one of those would otherwise never
		// start.
		said := &proseLines{stream: render.ToErr}
		drainer := *e
		drainer.Err = said
		drainer.PumpDeferred(ctx, repo, branches)
		rep.Drain = said.lines
	}
	return rep, nil
}

// minted reports whether a branch is one dockhand minted — the test
// every deletion in the pass is behind. The namespace is the whole of
// the answer: dockhand names what it makes, and a person's branch
// carrying a note is a person's branch still.
func minted(branch string) bool { return strings.HasPrefix(branch, git.BranchNamespace) }

// observedBranches is the pass's roster: the namespace first, in git's
// refname order, then every other local branch whose tip carries a
// verify note, in the same order.
//
// The extras are found by intersection rather than by lookup — one
// listing of every local branch with its tip, one listing of every
// noted commit, and a set membership per branch — so that a checkout
// with many hand-made branches pays two git calls for the answer and
// not a `git notes show` per branch. The primary branch is left out
// whatever its tip carries: it is nobody's change, and a note on it
// (a `verify <portdir>` of the tree as it stands) describes no branch
// the report could act on or a person could promote. A repository
// with no discernible primary excludes nothing, which is the honest
// reading of not knowing.
//
// A consequence worth stating: the extras are matched on the TIP. A
// hand-made branch verified and then amended drops out of the view
// again — its new tip has no note — where a dockhand/* branch would
// show its drift line. That is the fold-in's cheapness, chosen with
// its eyes open: the namespace is what dockhand promised to watch, and
// a noted hand-made tip is what `verify` promised to show.
func (e *Engine) observedBranches(ctx context.Context, repo *git.Repo) ([]string, error) {
	named, err := repo.Branches(ctx, git.BranchNamespace)
	if err != nil {
		return nil, err
	}
	noted, err := e.Ledger(repo).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(noted) == 0 {
		return named, nil
	}
	tips, err := repo.BranchTips(ctx, "")
	if err != nil {
		return nil, err
	}
	primary, _ := repo.PrimaryBranch(ctx)
	hasNote := make(map[string]bool, len(noted))
	for _, sha := range noted {
		hasNote[sha] = true
	}
	for _, bt := range tips {
		if minted(bt.Name) || bt.Name == primary || !hasNote[bt.Tip] {
			continue
		}
		named = append(named, bt.Name)
	}
	return named, nil
}

// pushedAmong maps each observed branch some remote holds a copy of to
// that remote: one ref listing for the namespace, where asking each
// branch in turn was a git call per branch, and one PushedTo per extra
// — the extras are few, and a namespace-scoped listing cannot see
// them. git.PushedTo says why the remote-tracking ref, and not the
// tracking config, is what "pushed" means.
func (e *Engine) pushedAmong(ctx context.Context, repo *git.Repo, branches []string) (map[string]string, error) {
	pushed, err := repo.Pushed(ctx, git.BranchNamespace)
	if err != nil {
		return nil, err
	}
	for _, br := range branches {
		if minted(br) {
			continue
		}
		remote, err := repo.PushedTo(ctx, br)
		if err != nil {
			return nil, err
		}
		if remote != "" {
			pushed[br] = remote
		}
	}
	return pushed, nil
}

// retire judges one branch's pull request and, under `cycle`, acts on
// the merged verdict — the one deletion a pass performs, because a
// merged PR is GitHub's own word that the work landed. Everything else
// stays reporting: open PRs, closed ones, and unpromoted branches say
// their state and stand.
//
// Under `status` nothing is acted on and the merged line names the
// verb that would (D27). Under `status --no-update` the forge is not
// even asked: the line says the pull request was not checked, which is
// a different sentence from there being none.
func (e *Engine) retire(ctx context.Context, repo *git.Repo, f *forge, b *render.BranchReport, o ReconcileOpts, now time.Time) {
	remote, pushed := f.pushed[b.Branch]
	if !pushed {
		// Never pushed: no remote holds a copy, so there is no pull request
		// to ask about, and no reason to spend a gh call finding that out.
		// Pushed reads the remote-tracking ref and not the tracking config,
		// which a branch cut from origin/master carries before it has ever
		// left the machine — git.PushedTo says why.
		return
	}
	b.Retire.Promoted, b.Retire.Minted = true, b.Minted
	if o.NoUpdate {
		b.Retire.Unasked = true
		return
	}
	if err := f.load(ctx); err != nil {
		b.Retire.Err = err.Error()
		return
	}
	pr, found, err := gh.LookupPR(ctx, e.Gh, f.remotes, remote, f.upstream, b.Branch)
	if err != nil {
		b.Retire.Err = err.Error()
		return
	}
	if !found {
		return
	}
	b.Retire.PR, b.PR = PRFact(pr, true), pr
	b.PRCreatedAt = prCreatedAt(pr)
	if b.Retire.PR.Open {
		// Only an open pull request has a window left to be inside or past,
		// so this Portfile read costs one blob per open PR, which is the
		// handful the reader is waiting on.
		b.Tier = portTier(ctx, repo, b.Branch, b.Note)
	}
	d := verdict.DecideRetire(true, b.Retire.PR)
	// The audit is closed from the verdict rather than from the
	// demolition, and before it, and by every verb that reached the
	// verdict. From the verdict, because a merged pull request is the
	// change's outcome whether or not the deletion was asked for, and
	// because a rejection retires nothing at all yet is exactly the
	// outcome worth counting. Before it, because the row is keyed by the
	// tip, and the demolition is about to take away the branch that tip
	// is reached through. By `status` too, because this is bookkeeping
	// of what the forge said — the ledger recording a world that already
	// changed — which is the write `status` keeps (D27).
	e.settleOutcome(ctx, repo, b, d, pr.MergeSha, now)
	if !o.Retire {
		return
	}
	if !b.Minted {
		// Deletion stays in the namespace (D27's fold-in): the line words
		// why, and the branch stands. Asked before --keep-merged is, so
		// that a hand-made branch is never said to be kept by a flag —
		// it would stand without one, and the line must say the reason
		// that is true.
		return
	}
	if !d.Cleans(o.KeepMerged) {
		if d == verdict.RetireMerged {
			b.Retire.Withheld = "--keep-merged"
		}
		return
	}
	if e.heldFromDeletion(ctx, repo, b) {
		return
	}
	said, derr := e.Discard(ctx, repo, b.Branch, true)
	b.Prose = append(b.Prose, said...)
	if derr != nil {
		// The pass reached the verdict and could not carry it out,
		// which is a different sentence from not having reached one.
		b.Retire.CleanErr = derr.Error()
		return
	}
	b.Retire.Cleaned = true
}

// heldFromDeletion asks the hold, at the last moment before a branch is
// demolished, and reports whether it said no.
//
// It withholds the DELETION and touches nothing else — the same shape
// --keep-merged has, and for the same reason: what a merged pull
// request means does not depend on whether anybody is willing to act on
// it. The audit row is already closed by the time this is asked, which
// is deliberate. A merge is the change's outcome whether or not the
// branch survived it, and a hold that also stopped the bookkeeping would
// make held changes disappear from the one record that counts them.
//
// It is asked here, at the one call site of the demolition, rather than
// once at the top of retire, so that the note is consulted only when a
// deletion is actually imminent. A branch whose note could not be read
// during observation is read again here, once, for the same reason.
//
// A note that cannot be read is not a hold. The branch is about to be
// deleted on GitHub's own word that the work landed, and inventing a
// hold out of an unreadable note would strand exactly the branches whose
// records are already in trouble.
func (e *Engine) heldFromDeletion(ctx context.Context, repo *git.Repo, b *render.BranchReport) bool {
	n := b.Note
	if n == nil {
		tip := b.Tip
		if tip == "" {
			var err error
			if tip, err = repo.RevParse(ctx, b.Branch); err != nil {
				return false
			}
		}
		read, err := e.Ledger(repo).Read(ctx, tip)
		if err != nil {
			return false
		}
		n = &read
	}
	err := GateHold(*n, b.Branch, "the deletion")
	if err == nil {
		return false
	}
	// Said where it changed what happened, and only there: this is
	// reached solely on the verdict that would have deleted, so the line
	// always reports a hold that actually stopped something. Said twice
	// on purpose — the prose is the pass reporting that it obeyed a
	// person, and the branch's own line must say why the branch is still
	// there (no kept case may be silent, D27).
	b.Prose = append(b.Prose, render.Line{Stream: render.ToErr, Text: err.Error()})
	var held *HeldError
	if errors.As(err, &held) {
		b.Retire.Withheld = "held (" + held.because() + ")"
	}
	return true
}

// settleOutcome closes this branch's audit row with what the forge said
// became of it. Only the two verdicts that are outcomes reach the log: a
// merge and a rejection are what a published change ends as, while an
// open pull request, a missing one and an unpromoted branch are all
// still in progress and have nothing to close.
//
// A branch this dockhand never published carries no rows and settles
// nothing, which is why the sha is asked for only after the verdict
// says a row could exist. The tip was observed by the same pass; a
// branch whose observation failed before it resolved a tip pays one
// rev-parse for it here.
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

// prCreatedAt parses the forge's creation timestamp into the time an
// age is measured from, here at the boundary where its JSON is already
// being read — the same reason PRFact maps its state words here.
//
// A timestamp that is missing or that GitHub spelled some way this does
// not read comes back as the zero time, which every reader of it treats
// as "unknown" rather than as "the year one". The error is dropped on
// purpose and the zero value carries the whole answer: there is exactly
// one thing a caller can do about an unparseable created_at, and it is
// what the caller does about an absent one.
func prCreatedAt(pr gh.PullRequest) time.Time {
	t, err := time.Parse(time.RFC3339, pr.CreatedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// forge is what looking a pull request up needs besides the branch:
// which branches have a copy on a remote at all, and the upstream
// repository and remote table, resolved once for the pass.
//
// The coordinates load lazily, because a namespace where nothing was
// ever pushed must cost no gh call at all — that is the common shape on
// a machine with no network and a pocket of local work, and paying for
// a lookup nobody will use is how a report becomes something to avoid
// running. The pushed set is not lazy: it is a local ref listing, and it
// is what decides whether the coordinates are needed.
type forge struct {
	repo *git.Repo
	// pushed maps each observed branch that some remote holds a copy of
	// to that remote — pushedAmong's answer. It gates everything a
	// lookup does: a branch with no copy anywhere has no pull request to
	// ask about, and the remote holding the copy is the fork the lookup
	// reads its owner from.
	pushed   map[string]string
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
