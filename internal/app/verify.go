package app

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Verify is the operation behind `verify <branch|port|subport|portdir>`.
// ONE ROAD, THREE SUBJECTS, one record shape: a change that exists; a
// branch or tree with no record (adopt); a branch a person continued
// past its record (follow — `verify` is the ask that lets the record
// follow, never a pass). It REQUIRES A REPOSITORY: a portdir outside any
// is refused (band 40) — "nothing here can record what it starts" — and
// `exec` outside a repository remains the untracked road for an ad-hoc
// guest; inside one it records a lease (ruled 2026-09-07). It REQUIRES A
// PROVIDER: on a host with none it refuses (33) and enqueues nothing,
// where Change mints and says unverified — a verify with nothing to
// verify against has no branch to leave behind as its excuse.
//
// This retires run.Verify, run.Foreground, the synchronous ask, and with
// it exit 36's last producer (Q16: nothing emits it). Destination is not
// touched: the attempt's existence is the ask (shipped askVerdict
// deletes). The grid's begin tick comes off: verify meets a change that
// exists, or adopts one. A PERSON's hold is refused at resolve (exit 23,
// change.Held with ActVerify); a crossing's is not a hold on a build.
type Verify struct {
	Repo      *git.Repo
	Ledger    *ledger.Ledger
	State     *statestore.Store
	Stage     run.Stager
	Local     run.Local
	Verifier  func(context.Context) (verify.Verifier, error)
	Me        record.OwnerID
	Residency func(context.Context) Residency
	Now       func() time.Time
	Progress  progress.Sink
	// Out is where --trace streams. A writer and not a Sink because a
	// build log is bytes passed through, not narration.
	Out io.Writer
}

// VerifyRequest is the one place the invocation-level plural lives:
// Platforms is `--on <release>[,<release>]|all`, resolved by cli, one
// attempt per entry in ONE Amend. Trace implies Wait and refuses more
// than one release.
type VerifyRequest struct {
	Target    string
	Platforms []platform.Release
	Test      bool
	KeepEnv   bool
	Trace     bool
	Wait      *time.Duration
	Residency Residency
}

// VerifyResult is one row per platform. Exit is 0 if every attempt
// started, 60 if any stayed queued, the verdict band under --wait; the
// remedy line is by residency.
type VerifyResult struct {
	Change   change.Ref
	Adopted  bool // a record was written for a branch or tree dockhand had not tracked, or followed a person's commit
	Attempts []Result
}

// Exit is the worst band any of this verify's rows landed in. It is a
// maximum over the codes and not over the Realizations, because the
// codes are what a script reads and the table that orders them is
// Result.Exit's.
func (v VerifyResult) Exit() int {
	worst := 0
	for _, a := range v.Attempts {
		if e := a.Exit(); e > worst {
			worst = e
		}
	}
	return worst
}

// Run: provider present, or refuse; resolve, and refuse a person's
// hold; stale + finish + release over former tips; adopt if there is no
// record (change.Snapshot for a tree — a pure object writer — or the
// branch's own tip), follow if a person continued the branch past its
// record (change.FollowIn); ONE Amend: the begin mutator (AdoptIn or
// FollowIn: the record and its ref line in the same batch) then N
// run.EnqueueIn (Adoptable asked INSIDE it over tx.State(), announced
// after); resolve — the Ref is the witness that the batch landed; start
// each; watch by residency; --trace adds run.Follow, which judges
// nothing.
func (v Verify) Run(ctx context.Context, r VerifyRequest) (VerifyResult, error) {
	var res VerifyResult
	prov, err := provider(ctx, v.Verifier)
	if err != nil {
		return res, err // 33: nothing to verify against, nothing enqueued
	}
	ref, err := change.Resolve(ctx, v.Repo, v.State, r.Target)
	st, rerr := v.State.Read(ctx)
	if rerr != nil {
		return res, rerr
	}
	var begin func(tx *statestore.Txn) error // AdoptIn or FollowIn, or nil
	var id record.ChangeID
	var tip string
	var content record.ContentID
	// subjects is what the ENQUEUE's roster is computed from, and it is
	// read off the record for a change that exists and derived from git
	// for one being adopted. It is needed here rather than left to
	// run.Roster inside the closure because a Spec's id covers the roster
	// by identity, and the id has to be the one a later drain re-derives.
	var subjects []record.Subject
	followed := false
	switch {
	case err == nil:
		c := st.Changes[string(ref.ID())]
		if err := change.Held(c, change.ActVerify, record.Human); err != nil {
			return res, err // 23: a person's hold
		}
		// stale: the tip moved past these. Finish(Superseded) per Active,
		// lease.Release per kept lease — the verdict stands.
		if err := supersede(ctx, v.State, v.Ledger, prov, v.Local, st, ref.ID(), ref.Tip(), v.Claimant(), v.Me, v.Now); err != nil {
			return res, err
		}
		id, tip, content, subjects = ref.ID(), ref.Tip(), ref.Content(), c.Subjects
	case isNoRecord(err):
		// adopt: a dockhand/ branch with no record, or a working tree. The
		// object is written here; the pin or the assert line is AdoptIn's,
		// in the Amend below.
		id = newChangeID()
		branch := branchOf(r.Target) // "" for a portdir
		var base record.Base
		if branch == "" {
			tip, content, err = change.Snapshot(ctx, v.Repo, id, r.Target)
			if err == nil {
				subjects, base, err = snapshotSubject(ctx, v.Repo, r.Target)
			}
		} else {
			tip, content, err = tipOf(ctx, v.Repo, branch)
			if err == nil {
				subjects, base, err = branchSubjects(ctx, v.Repo, tip)
			}
		}
		if err != nil {
			return res, err
		}
		a := change.Adoption{
			ID: id, Branch: branch, Tip: tip, Content: content, Subjects: subjects, Base: base,
			Prov: change.Provenance{AskedBy: record.Human, Via: record.MintedAdopted},
		}
		begin = func(tx *statestore.Txn) error { _, err := change.AdoptIn(tx, a, v.Now()); return err }
		res.Adopted = true
	case isTipDisagrees(err):
		// follow: a branch a person continued. The record catches up to
		// the ref, and the assert line refuses if the ref moved again.
		d := disagreementOf(err)
		c, ok := st.Changes[string(d.ID)]
		if !ok || c.Branch == "" || d.Absent {
			return res, err // a hand-moved pin, or a deleted branch: 45, reported; discard is the remedy
		}
		if err := change.Held(c, change.ActVerify, record.Human); err != nil {
			return res, err
		}
		cont, err := contentOf(ctx, v.Repo, d.Found)
		if err != nil {
			return res, err
		}
		id, tip, content, subjects = c.ID, d.Found, cont, c.Subjects
		via := change.Provenance{AskedBy: record.Human, Via: record.MintedAdopted}
		begin = func(tx *statestore.Txn) error {
			_, err := change.FollowIn(tx, c.ID, d.Found, cont, via, v.Now())
			return err
		}
		res.Adopted, followed = true, true
	default:
		return res, err
	}

	// ONE Amend: begin (record + ref line), then N run.EnqueueIn over
	// id/tip/content; Adoptable asked INSIDE it over tx.State(), its
	// answers captured as data.
	var atts []record.Attempt
	var adopted []record.Attempt
	specs := map[string]run.Spec{}
	err = v.State.Amend(ctx, func(tx *statestore.Txn) error {
		if begin != nil {
			if err := begin(tx); err != nil {
				return err
			}
		}
		atts, adopted = atts[:0], adopted[:0]
		for _, pl := range r.Platforms {
			spec := run.Spec{
				Content: content, Roster: rosterOf(subjects), Platform: pl,
				Test: r.Test, KeepEnv: r.KeepEnv, Trace: r.Trace,
			}
			specs[pl.Name] = spec
			if a, ok := run.Adoptable(attempts(tx.State()), content, spec.ID(), pl, v.Now()); ok {
				adopted = append(adopted, a)
				atts = append(atts, a)
				continue
			}
			a, err := run.EnqueueIn(tx, run.Enqueue{Change: id, Sha: tip, Content: content, Spec: spec, Platform: pl, Ask: record.Ask{Test: r.Test, KeepEnv: r.KeepEnv}, EnqueuedBy: v.Me}, v.Now())
			if err != nil {
				return err
			}
			atts = append(atts, a)
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	// resolve: the batch created the pin, asserted the branch, or moved
	// nothing; app holds no Ref it did not resolve. A foreign move in the
	// second between is reported here (45).
	if ref, err = change.Resolve(ctx, v.Repo, v.State, resolved(r.Target, id)); err != nil {
		return res, err
	}
	res.Change = ref
	// the supersede stage for a FOLLOW runs HERE, after the Amend, over
	// the former tip's attempts — the old tip is now a former tip.
	if followed {
		st2, err := v.State.Read(ctx)
		if err != nil {
			return res, err
		}
		if err := supersede(ctx, v.State, v.Ledger, prov, v.Local, st2, ref.ID(), ref.Tip(), v.Claimant(), v.Me, v.Now); err != nil {
			return res, err
		}
	}
	for _, a := range adopted {
		say(v.Progress, progress.Info, "adopted attempt "+a.ID+" started "+a.Started.Format(time.RFC3339))
	}

	// start each; the first ErrNoVacancy leaves the rest Queued.
	full := false
	for _, a := range atts {
		row := Result{Did: Queued, Ref: ref, Attempt: a.ID}
		if full {
			res.Attempts = append(res.Attempts, row)
			continue
		}
		started, err := run.Start(ctx, v.State, prov, v.Stage, a, v.Claimant(), v.Now())
		switch {
		case err == nil:
			row.Did, row.Lease = Started, leaseOf(started)
			if r.Wait != nil {
				if r.Trace {
					v.follow(ctx, prov, started)
				}
				final, err := watch(ctx, v.State, v.Ledger, prov, v.Local, started, specs[a.Platform], *r.Wait, r.Residency, v.Residency, v.Claimant(), v.Now)
				if err != nil {
					return res, err
				}
				if final.Phase == record.Finished {
					row.Did, row.Verdict = Stood, verdictOf(final)
				}
			}
		case isNoVacancy(err):
			full = true
		case isNoEnvironment(err):
			row.Deferred = &Deferral{Reason: NoEnvironment, Detail: err.Error()}
		default:
			return res, err
		}
		res.Attempts = append(res.Attempts, row)
	}
	return res, nil
}

// follow streams the guest's log under --trace, in a goroutine, and
// judges nothing (R10): the verdict is the record's and the watcher's
// role is chosen by residency one call below.
//
// It reads the LEASE the start just wrote, because run.Follow takes the
// environment and not the attempt — the attempt names its lease by the
// store's key, and the provider's handle is on the lease. That is one
// extra read, on the --trace road only, and it is the honest cost of the
// attempt carrying a token rather than a provider's name.
func (v Verify) follow(ctx context.Context, prov verify.Verifier, a record.Attempt) {
	st, err := v.State.Read(ctx)
	if err != nil {
		return
	}
	l, ok := st.Leases[a.Lease]
	if !ok || v.Out == nil {
		return
	}
	go func() { _ = run.Follow(ctx, prov, l, v.Out) }()
}

// supersede is the stale + finish + release stage shared by Verify,
// Accept, Cancel and Discard: run.Stale (pure) over one read, then
// run.Finish with Interrupt Superseded per Active attempt on a former
// tip, and lease.Release per kept lease on a finished one, with its
// verdict standing. It takes the record's id and the tip that now
// stands, never a Ref: a road that resolved a disagreement still owes
// this over the recorded tip's work.
func supersede(ctx context.Context, st *statestore.Store, l *ledger.Ledger, prov verify.Verifier, local run.Local, s statestore.State, id record.ChangeID, tip string, by lease.Claimant, me record.OwnerID, now func() time.Time) error {
	c := s.Changes[string(id)]
	stale := run.Stale(s, c, tip)
	for _, a := range stale.Active {
		itr := &record.Interrupt{Why: record.InterruptSuperseded, By: me, At: now(), Detail: "the branch moved to " + tip}
		if _, err := run.Finish(ctx, st, l, prov, local, a, specOf(s, a), itr, by, now); err != nil {
			return err
		}
	}
	for _, k := range stale.Kept {
		if err := lease.Release(ctx, st, prov, k.Change, k.Platform, by, now); err != nil {
			return err
		}
	}
	return nil
}

// Claimant is who this verify is, for the lease its starts acquire.
func (v Verify) Claimant() lease.Claimant { return lease.Claimant{Owner: v.Me} }

func isNoRecord(err error) bool { return errors.Is(err, change.ErrNoRecord) }

// branchOf is the branch a target names, and "" for anything else. It
// answers only for a name inside dockhand's own namespace: a bare port,
// a portdir path and a sha are not branches, and adopting one as a
// branch would write a record binding a name nothing carries.
func branchOf(target string) string {
	if strings.HasPrefix(target, git.BranchNamespace) {
		return target
	}
	return ""
}

// resolved is the name the post-Amend Resolve is given. A branch or a
// port resolves by the name the caller typed; a working-tree snapshot
// has no such name — the record it just wrote is branchless — so it is
// named by its pin, which is Resolve's fourth target form.
func resolved(target string, id record.ChangeID) string {
	if branchOf(target) == "" && looksLikePath(target) {
		return change.PinRef(id)
	}
	return target
}

// looksLikePath is the one syntactic question this road asks of its
// target, and it asks it for a reason a lookup could not answer: a
// portdir was just SNAPSHOTTED, so there is no branch and no record to
// resolve it by, and the pin is the only name the new record has.
func looksLikePath(target string) bool {
	return strings.HasPrefix(target, "/") || strings.HasPrefix(target, ".") || strings.Contains(target, "/")
}

// tipOf reads a branch's commit and the tree it names — RevParse of the
// branch and of <sha>^{tree} — which is what an adoption records as its
// content: the identity of the files, not of the commit that carries
// them, so a rebase that changes nothing changes no content.
func tipOf(ctx context.Context, repo *git.Repo, branch string) (string, record.ContentID, error) {
	sha, err := repo.RevParse(ctx, branch)
	if err != nil {
		return "", "", err
	}
	content, err := contentOf(ctx, repo, sha)
	return sha, content, err
}

// contentOf is the content identity of a commit: the oid of the tree it
// carries, unadorned, because that is what change.Prepared.Identify
// produces (git.Repo.GraftTree's oid) and what every record.ContentID in
// the store already is. A prefix here would make an adopted branch's
// content unequal to the identical minted one's, and Adoptable matches
// on content.
func contentOf(ctx context.Context, repo *git.Repo, sha string) (record.ContentID, error) {
	tree, err := repo.RevParse(ctx, sha+"^{tree}")
	if err != nil {
		return "", err
	}
	return record.ContentID(tree), nil
}

// branchSubjects derives what an adopted BRANCH changes, from git alone,
// against its merge base with the primary branch: change.ChangedPortdirs
// is the audit, and here it runs with no record to hold the answer
// against, which is exactly the case its doc names — "a record that
// names no portdir at all is not a disagreement ... git's answer stands
// unopposed".
//
// NAMES IS LEFT NIL, and that is rule 7 rather than an omission. A
// subject's Names is the port and its subports, and record.Subject says
// an empty slice means "nobody ever asked" while [Port] means "asked,
// and there are none". Naming the subports needs an evaluation, and this
// operation holds no evaluator — Verify's dependencies are a repository,
// a store, a stager and a provider — so the honest answer is the one
// that says nobody asked.
func branchSubjects(ctx context.Context, repo *git.Repo, tip string) ([]record.Subject, record.Base, error) {
	base, err := baseOf(ctx, repo, tip)
	if err != nil {
		return nil, record.Base{}, err
	}
	dirs, err := change.ChangedPortdirs(ctx, repo, record.Change{Tip: tip}, base.Sha)
	if err != nil {
		return nil, record.Base{}, err
	}
	subjects := make([]record.Subject, 0, len(dirs))
	for _, dir := range dirs {
		subjects = append(subjects, record.Subject{Port: path.Base(dir), Portdir: dir})
	}
	return subjects, base, nil
}

// snapshotSubject is the working tree's one subject: the portdir a
// person pointed at, tree-relative because that is what a commit names
// and what the Stager materializes by. Names is nil for the reason
// branchSubjects gives.
func snapshotSubject(ctx context.Context, repo *git.Repo, portdir string) ([]record.Subject, record.Base, error) {
	rel, err := repo.RelPath(portdir)
	if err != nil {
		return nil, record.Base{}, err
	}
	base, err := baseOf(ctx, repo, "HEAD")
	if err != nil {
		return nil, record.Base{}, err
	}
	return []record.Subject{{Port: path.Base(rel), Portdir: rel}}, base, nil
}

// baseOf is the commit an adoption is measured from: the merge base of
// the tip with this checkout's primary branch, with the moment it landed
// — the same pair change.Prepare records for a minted change, so an
// adopted change and a minted one answer change.Behind identically.
func baseOf(ctx context.Context, repo *git.Repo, rev string) (record.Base, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return record.Base{}, err
	}
	sha, err := repo.MergeBase(ctx, rev, primary)
	if err != nil {
		return record.Base{}, err
	}
	at, err := repo.CommittedAt(ctx, sha)
	if err != nil {
		return record.Base{}, err
	}
	return record.Base{Sha: sha, CommittedAt: at}, nil
}

// isTipDisagrees and disagreementOf are the one place app reads
// change.ErrTipDisagrees: Verify's follow, Discard's absent-ref road and
// Status's Disagreeing row all branch through them.
func isTipDisagrees(err error) bool { return errors.Is(err, change.ErrTipDisagrees) }

func disagreementOf(err error) *change.TipDisagreement {
	var d *change.TipDisagreement
	errors.As(err, &d)
	return d
}
