package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// planOnBase resolves a plan against the repository it will land in:
// the repo, its primary branch, the Portfile's repo-relative path, and
// the edited bytes — computed from the base commit's blob, never the
// working file, with the plan's precondition hash held against that
// blob. Both realizations that speak git — mint and diff — start here.
// The repository it opens is the run's, resolved once, and anchored on
// the portdir the plan named: an intent may name one outside the tree.
func (e *Engine) planOnBase(ctx context.Context, p *plan.Plan) (repo *git.Repo, primary, path string, edited []byte, err error) {
	repo, err = e.RepoFor(ctx, p.Portdir)
	if err != nil {
		if errors.Is(err, git.ErrNotARepo) {
			// Wrapped, not swallowed: the identity is what routes this
			// to the tree exit band.
			return nil, "", "", nil, fmt.Errorf("%w — the branch workflow needs a git checkout; --in-place edits the tree directly", err)
		}
		return nil, "", "", nil, err
	}
	primary, err = repo.PrimaryBranch(ctx)
	if err != nil {
		return nil, "", "", nil, err
	}
	rel, err := repo.RelPath(p.Portdir)
	if err != nil {
		return nil, "", "", nil, err
	}
	path = rel + "/" + macports.PortfileName
	base, err := repo.BlobAt(ctx, primary, path)
	if err != nil {
		return nil, "", "", nil, err
	}
	edited, err = p.Materialize(base)
	if errors.Is(err, plan.ErrDrift) {
		return nil, "", "", nil, fmt.Errorf("%w: the Portfile on %s is not the one planned against — commit your work there first, or use --in-place", plan.ErrDrift, primary)
	}
	if err != nil {
		return nil, "", "", nil, err
	}
	return repo, primary, path, edited, nil
}

// BranchInFlightError is the refusal an intent gives when its port
// already has a branch: refusal is a feature, not a failure — the user
// asked for one thing, and the reason they did not get it is a
// judgment with a remedy, not something broken.
type BranchInFlightError struct {
	Branch string
	// Reason overrides the default message: --replace's narrower refusal
	// speaks here.
	Reason string
}

func (e *BranchInFlightError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return fmt.Sprintf("a change for this port is already in flight: %s — discard it, pick up where it left off, or --replace to replace it", e.Branch)
}

// DockhandExit: its own code inside the declined band. A caller sweeping
// ports needs "already in flight" apart from every other decline —
// the remedy is a different verb, not a different request.
func (e *BranchInFlightError) DockhandExit() int { return exitcode.BranchInFlight }

// Code names the refusal for a machine.
func (e *BranchInFlightError) Code() string { return "branch-in-flight" }

// replaceInFlight clears the way for --replace: the standing branch goes
// through discard's own demolition — running verification canceled,
// workers released, notes removed — but only when the branch is
// exactly what dockhand minted. Commits the user added are theirs;
// destroying them silently is what the refusal exists to prevent, and
// discard remains the explicit act for that.
func (e *Engine) replaceInFlight(ctx context.Context, repo *git.Repo, primary, branch string) error {
	own, err := repo.OwnCommits(ctx, branch, primary)
	if err != nil {
		return err
	}
	if len(own) > 1 {
		return &BranchInFlightError{Branch: branch, Reason: fmt.Sprintf(
			"%s carries %d commit(s) beyond the mint — --replace replaces only what dockhand placed; `dockhand discard %s` first if you mean to drop your own work",
			branch, len(own)-1, branch)}
	}
	fmt.Fprintf(e.Err, "replacing in-flight %s (--replace)\n", branch)
	// The demolition's own words follow the announcement, on the streams
	// they chose: --replace is one of the four places discard's report
	// lands, and it lands here rather than being swallowed because the
	// user asked to destroy a branch and is owed the sentence saying it
	// happened. Printed even when the demolition failed — the half it
	// did is still what it did.
	said, err := e.Discard(ctx, repo, branch, false)
	render.Prose(said, e.Out, e.Err)
	return err
}

// Minted is what a realized branch hands back: enough for the caller
// to submit verification against the sha and tell the user where the
// change lives.
type Minted struct {
	Repo    *git.Repo
	Branch  string
	Sha     string // full commit sha
	RelPort string // repo-relative portdir path
	// Stood says nothing was minted because the branch this change
	// would have created is already standing, and the policy asked to
	// advance past it rather than refuse. Only Branch is filled.
	Stood bool
	// Superseded names the port's other in-flight branches, now
	// recorded as replaced by this one. They still exist and still hold
	// what they learned; the field is here so a sweep can say a port's
	// older change was set down rather than leaving it to be discovered
	// in a note.
	Superseded []string
}

// mint realizes a plan as a branch (D21): the edited Portfile is
// committed onto the tree's primary branch at its local position,
// under dockhand's namespace, entirely in the object database — the
// user's HEAD and working tree are never touched. A plan with no edits
// mints nothing and returns nil, nil.
//
// It also bears the commit's record. Schema 3's record is born here
// rather than at the first submit, because everything a subject knows
// is known here and nowhere later: the directory the change touched,
// the intent that made it, what it moves to, and how far the
// invocation's contract reaches. A branch minted with --no-verify
// submits nothing at all, so a record that waited for a job would have
// no place to keep any of it — and a --no-verify branch therefore
// gains a note where it used to have none.
func (e *Engine) mint(ctx context.Context, p *plan.Plan, o Policy) (*Minted, error) {
	branch, message := git.MintBranchName(p.Slug), commitMessage(p)
	replace := o.OnInFlight == Replace
	hasEdits := len(p.Edits) > 0
	// The decision is asked twice, and the order is the reason: the
	// empty-plan answer is reached before the plan is resolved against
	// the repository at all, so a plan with nothing in it never reports
	// drift, while the branch probe below happens after, so a drift
	// refusal precedes a replacement. The first call cannot need the
	// probe; the second is the same question with the probe's answer in
	// it.
	switch verdict.DecideMint(hasEdits, replace, false) {
	case verdict.NothingToMint:
		// A no-op realized as a branch would be an empty commit.
		fmt.Fprintln(e.Out, "no edits; no branch minted")
		return nil, nil
	case verdict.NothingToReplace:
		fmt.Fprintln(e.Out, "no edits; no branch minted")
		fmt.Fprintln(e.Err, "an existing in-flight branch, if any, stands: --replace replaces only when there is something to replace it with")
		return nil, nil
	case verdict.MintBranch, verdict.ReplaceThenMint:
		// Both mint; which of the two it is cannot be settled until the
		// plan has been resolved against the repository below.
	}
	if o.OnInFlight == Advance {
		// Advance asks one question and asks it before anything else:
		// is the branch this change would create already standing? A
		// yes ends it — there is nothing to write, so there is nothing
		// to resolve against the tree and no drift to report about a
		// file nobody is going to touch. That is what makes a rerun
		// cheap on the ports an interrupted sweep already minted, and
		// it is the whole difference between this and Supersede, which
		// has a branch to mint and must do the work.
		//
		// A repository that will not open falls through to the road
		// below, where the failure is reported properly rather than
		// read as "no branch".
		if repo, err := e.RepoFor(ctx, p.Portdir); err == nil && repo.HasBranch(ctx, branch) {
			return &Minted{Repo: repo, Branch: branch, Stood: true}, nil
		}
	}
	repo, primary, path, edited, err := e.planOnBase(ctx, p)
	if err != nil {
		return nil, err
	}
	hasBranch := verdict.MintProbesBranch(hasEdits, replace) && repo.HasBranch(ctx, branch)
	if verdict.DecideMint(hasEdits, replace, hasBranch) == verdict.ReplaceThenMint {
		if err := e.replaceInFlight(ctx, repo, primary, branch); err != nil {
			return nil, err
		}
	}
	sha, err := repo.Mint(ctx, git.MintRequest{
		Branch: branch,
		Base:   primary,
		// One commit carrying one file: a plan is one port's edit, and
		// the chain and the file list are both at their length of one.
		Commits: []git.Commit{{
			Files:   []git.File{{Path: path, Content: edited}},
			Message: message,
		}},
	})
	if err != nil {
		if errors.Is(err, git.ErrBranchExists) {
			if o.sweeping() {
				// Supersede's own encounter with the branch it would
				// have minted, and Advance's when something minted it
				// between the probe and here. A sweep racing another
				// sweep — or itself, rerun — is still a sweep meeting a
				// branch that already carries this change, and that is
				// an answer rather than a failure.
				return &Minted{Repo: repo, Branch: branch, Stood: true}, nil
			}
			return nil, &BranchInFlightError{Branch: branch}
		}
		return nil, err
	}
	rel, err := repo.RelPath(p.Portdir)
	if err != nil {
		return nil, err
	}
	m := &Minted{Repo: repo, Branch: branch, Sha: sha, RelPort: rel}
	e.bear(ctx, m, p, primary, o.destination())
	m.Superseded = e.supersedeSiblings(ctx, repo, branch, p.Port)
	fmt.Fprintf(e.Out, "branch: %s (%s)\n", branch, git.Abbrev(sha))
	fmt.Fprintf(e.Err, "your checkout is untouched — `git checkout %s` to add changes\n", branch)
	return m, nil
}

// tracTicket is where a MacPorts ticket lives. The pull request body
// writes the same URL from the same number; until render owns the whole
// commit message, the two spellings are two — and this is the one that
// goes somewhere nothing rewrites.
const tracTicket = "https://trac.macports.org/ticket/"

// commitMessage is what the minted commit says: the plan's summary as
// its subject, and — when the intent was given a ticket — the Closes:
// trailer the project's commit guidelines ask for.
//
// It is composed here rather than by the planner because Summary is the
// plan's identity and a trailer is not: the plan's bytes are a hash
// gate, and a subject that grew a second paragraph would be a different
// plan for the same change. The ticket rides as its own field, and the
// realizer is where a field becomes a message.
//
// The full URL rather than the bare number, because that is what the
// guidelines ask for, and because the checklist box a reviewer ticks
// says "with full URL".
func commitMessage(p *plan.Plan) string {
	if p.ClosesTicket == "" {
		return p.Summary
	}
	// A blank line before it: git reads a trailer only in the last
	// paragraph, and a subject line is a paragraph. No trailing newline,
	// because git supplies one and a summary-only message has none —
	// the two messages differ by the trailer and by nothing else.
	return fmt.Sprintf("%s\n\nCloses: %s%s", p.Summary, tracTicket, p.ClosesTicket)
}

// bear opens the minted commit's record: who the change is about,
// where it is bound, who asked, and what it was minted on top of.
//
// A failure to write it is a warning and never the mint's answer, on
// the same terms as a deferral's note: the branch exists either way,
// and reporting a successful mint as a failure because a note could
// not be written would be the more misleading of the two. What is lost
// is the per-subject facts, which the branch's own diff can no longer
// supply — so it is said out loud rather than swallowed.
func (e *Engine) bear(ctx context.Context, m *Minted, p *plan.Plan, base string, dest record.Destination) {
	b := e.baseOf(ctx, m.Repo, base)
	if err := e.Ledger(m.Repo).Update(ctx, m.Sha, func(r *record.Record) error {
		r.Slug = p.Slug
		r.Subjects = []record.Subject{{
			Port: p.Port,
			// Written as [Port] and not left empty. The empty slice
			// already means something else — nobody asked — and the
			// planner did ask: it evaluated the Portfile and named the
			// one context this change moves.
			Names:   []string{p.Port},
			Portdir: m.RelPort,
			Intent:  p.Intent,
			Target:  targetIn(p.Slug, p.Port),
		}}
		// The housekeeping the change carried that nobody asked for,
		// remembered by rule name. The pull request body vouches for what
		// the note holds, so the names have to survive the mint — a diff
		// cannot say which of its hunks was a rider.
		r.Riders = p.Riders
		// What examining the port turned up, stamped as it is appended.
		// The judgment has no clock — a finding is made from bytes and
		// says nothing about when — so the moment is the realizer's, and
		// it is the moment the note learned it rather than the moment the
		// comment was written.
		r.Findings = stampFindings(p.Findings, time.Now())
		r.Destination = dest
		// The same ticket the trailer names, so that the pull request
		// body can cite it without being told again — and so that a
		// reader of the note can see what the commit claims to close.
		r.ClosesTicket = p.ClosesTicket
		// A person ran the verb. The machine value exists for the sweep,
		// which has no caller yet, and neither value is ever an input to
		// a gate: a field that could widen what the unattended road is
		// allowed to do would be an authorization rather than provenance.
		r.AskedBy = record.Human
		r.MintedVia = record.MintedSingle
		r.Base = b
		return nil
	}); err != nil {
		fmt.Fprintf(e.Err, "warning: recording the change on %s: %v\n", m.Branch, err)
	}
}

// baseOf reads the commit a change is minted on top of: the sha, and
// when that commit was made.
//
// Both halves answer different readers — the sha is the honest "before"
// a baseline is measured at, and the date is how a reader tells a
// change written against a week-old tree from one written against
// today's — and each is written only if it could be read. A base that
// cannot be resolved at all leaves the field absent rather than
// guessed, which is what omitzero on the record's side is for.
func (e *Engine) baseOf(ctx context.Context, repo *git.Repo, base string) record.Base {
	sha, err := repo.RevParse(ctx, base)
	if err != nil {
		return record.Base{}
	}
	at, err := repo.CommittedAt(ctx, sha)
	if err != nil {
		return record.Base{Sha: sha}
	}
	return record.Base{Sha: sha, CommittedAt: at}
}

// targetIn is what a change moves its port to — "1.9", "checksums",
// "rev2" — recovered from the two values the planner already holds.
//
// It is not the branch-name reading that Subject.Target exists to end.
// That one has a name and nothing else, and must split it somewhere; a
// slug and the port that built it are both here, so cutting the one
// off the other is inverting a construction rather than parsing a
// string. A slug that does not carry the port keeps nothing: a wrong
// target is worse than an absent one.
func targetIn(slug, port string) string {
	if port == "" {
		return ""
	}
	rest, _ := strings.CutPrefix(slug, port+"-")
	if rest == slug {
		return ""
	}
	return rest
}

// supersedeSiblings marks the other in-flight branches for this port as
// replaced by the one just minted.
//
// Port-keyed, because that is the relation the record's field names:
// dockhand/jq-1.8.2 and dockhand/jq-1.8.3 are one port under two branch
// names, and the in-flight refusal compares branch names, so both
// stand. The newer branch is the change now. The older one keeps
// everything it learned and gains the field that says why it will learn
// nothing more.
//
// At mint and never at submit. A submit happens to whichever branch was
// pointed at — the drain retries an old one, `dockhand verify` names
// one by hand — so writing the field from there would let an older
// branch declare a newer one superseded. A mint is the one moment where
// which of two branches is the newer is not a guess.
//
// Silent on success. There is no superseded phase in the report yet, so
// the note knows something the branch's line does not; saying it here
// instead would be that line, in the wrong place. What it marked comes
// back instead, in the order the branches were listed, for the sweep
// that must say it in a row rather than a sentence.
func (e *Engine) supersedeSiblings(ctx context.Context, repo *git.Repo, minted, port string) []string {
	if port == "" {
		return nil
	}
	branches, err := repo.Branches(ctx, git.BranchNamespace)
	if err != nil {
		return nil
	}
	var marked []string
	l := e.Ledger(repo)
	for _, br := range branches {
		if br == minted {
			continue
		}
		tip, err := repo.RevParse(ctx, br)
		if err != nil {
			continue
		}
		// Read before the update so that a branch about another port —
		// which is most of them — costs one note read and no lock.
		n, err := l.Read(ctx, tip)
		if err != nil || n.Headline().Port != port || n.SupersededBy == minted {
			continue
		}
		if err := l.Update(ctx, tip, func(r *record.Record) error {
			if r.Headline().Port != port || r.SupersededBy == minted {
				return ledger.ErrUnchanged
			}
			r.SupersededBy = minted
			return nil
		}); err != nil {
			fmt.Fprintf(e.Err, "warning: recording %s as superseded by %s: %v\n", br, minted, err)
			continue
		}
		marked = append(marked, br)
	}
	return marked
}
