// Package prepare turns a plan into the content a change will carry.
//
// IT IS NEITHER planning NOR change NOR app, and each of those was tried
// first. planning denies both — "the boundary between producer and
// consumer is this line" for change, and for git "a producer that read
// one could plan against a commit nobody asked for". change realizes
// plans already made and does not make them, which is what its upstream
// and vendored denials say. And app opens no files: preparing a member
// needs a blob read and an evaluation, which is exactly why
// app.Accept.Prepare is a seam rather than a call.
//
// So preparation is a consumer-owned seam like staging, and it gets a
// package for the same reason: it was implemented in the composition
// root because the root is what holds a repository, a temporary root, a
// planner and an evaluator at once — and none of it is wiring.
package prepare

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/planning"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// Preparer holds the four things preparation needs and the composition
// root has.
type Preparer struct {
	Repo *git.Repo
	Temp tempdir.Root
	Plan planning.Planner
	Eval change.Evaluator
}

// AtBase prepares a plan made against the WORKING TREE, holding it
// against a base commit. The two sources can disagree, and that is what
// change.Prepare's drift check is for; the words for a drift are the
// road's, so this returns the sentinel and says nothing about remedies.
func (p Preparer) AtBase(ctx context.Context, pl *plan.Plan, base record.Base, portdir change.TreePath) (change.Prepared, error) {
	blob, err := p.Repo.BlobAt(ctx, base.Sha, string(portdir)+"/"+macports.PortfileName)
	if err != nil {
		return change.Prepared{}, err
	}
	// The base's bytes for the whole files the plan rewrites, beside the
	// Portfile's: a patch relocated at plan time is derived from bytes
	// that must still be there.
	aux, err := baseFiles(ctx, p.Repo, base.Sha, string(portdir), pl.Files)
	if err != nil {
		return change.Prepared{}, err
	}
	return change.Prepare(ctx, pl, change.Source{
		Base: base, Portdir: portdir, Portfile: blob, Files: aux,
	}, p.Eval)
}

// Cohort plans every member of a revbump proposal from the TIP's own
// bytes and assembles them into one change.
//
// It is the implementation of app.Accept's Prepare seam, and it lives
// out here for the reason that seam exists: preparing a member needs a
// blob read and an evaluation, and an operation opens no files.
func (p Preparer) Cohort(ctx context.Context, tip string, cands []record.Candidate, criterion string) (change.Prepared, error) {
	members := bumped(cands)
	if len(members) == 0 {
		return change.Prepared{}, change.ErrEmptyCohort
	}
	at, err := p.Repo.CommittedAt(ctx, tip)
	if err != nil {
		return change.Prepared{}, err
	}
	// ONE PREPARE PER MEMBER, ASSEMBLED. It used to plan cands[0] and
	// stop, so a six-member cohort bumped one port and the refusal a
	// person met named the wrong limit ("this cohort spans 6 portdirs")
	// — two members sharing one portdir fared no better.
	//
	// Each member goes down exactly the road a solo revbump takes: the
	// same plan, the same precondition, the same drift check, the same
	// evaluation. change.Merge then makes them one change, which is
	// what a cohort is — a port's change and its dependents' changes.
	// THE MEMBERS ARE PLANNED FROM THE COMMIT, not from the working
	// tree, and the two used to be different sources.
	//
	// A record's Portdir is TREE-RELATIVE. It was handed to
	// tree.Target, whose Portdir is documented "absolute portdir
	// path", and travelled from there to the Tcl evaluator — which
	// resolved it against the PROCESS'S WORKING DIRECTORY. The cohort
	// road therefore planned against whatever sat at that relative
	// path beside wherever dockhand happened to be run, and worked
	// only because that is normally the tree root. Run with --tree
	// from anywhere else it evaluated the wrong file, or none, and the
	// refusal that reached the person blamed the PORT and offered
	// `--exclude` — which drops a dependent from a revbump, the exact
	// harm a cohort exists to prevent.
	//
	// Staging the tip's portdirs first settles both halves at once.
	// The path is constructed rather than inherited, so no working
	// directory is involved; and the bytes the planner evaluates are
	// the bytes Prepare is given below, so plan-source and
	// prepare-source cannot disagree — which is what the branch tip
	// being authoritative MEANS. run.Stager has staged builds this way
	// since it was written ("from the object database, never from the
	// working tree"); this is the same discipline reaching the road
	// that plans.
	root, drop, terr := p.Temp.MakeDir("cohort")
	if terr != nil {
		return change.Prepared{}, terr
	}
	defer drop()
	for _, c := range members {
		if merr := p.Repo.Materialize(ctx, tip, c.Portdir, root); merr != nil {
			return change.Prepared{}, fmt.Errorf("%s: staging %s from %s: %w", c.Port, c.Portdir, git.Abbrev(tip), merr)
		}
	}
	// _resources beside them, because a Portfile that opens with
	// `PortGroup github 1.0` cannot be evaluated without the tree's
	// group files. A tree that carries none is not a failure here: the
	// evaluation that needs one fails on its own terms, naming the
	// member, which is a better sentence than this could write.
	if merr := p.Repo.Materialize(ctx, tip, build.ResourcesDir, root); merr != nil {
		slog.Debug("cohort staging: no resources tree", "rev", tip, "err", merr)
	}

	parts := make([]change.Prepared, 0, len(members))
	for _, c := range members {
		dir := c.Portdir
		staged := filepath.Join(root, filepath.FromSlash(dir))
		blob, berr := os.ReadFile(filepath.Join(staged, macports.PortfileName))
		if berr != nil {
			return change.Prepared{}, berr
		}
		pl, perr := p.Plan.Plan(ctx, "bump-revision", stagedTarget(staged, c), cohortParams(dir, cands, criterion))
		if perr != nil {
			// A MEMBER THAT WILL NOT PLAN NAMES THE ROAD PAST ITSELF.
			// One member's Portfile can defeat the revision edit — a
			// revision driven by a variable, a PortGroup, a conditional —
			// and the other five are fine. dockhand does NOT drop it
			// quietly: a dependent silently left out of a revbump cohort
			// ships stale against a library that moved, which is the exact
			// harm the cohort exists to prevent. So the person is told
			// which member, why, and the one flag that proceeds without
			// it.
			return change.Prepared{}, fmt.Errorf("%s: %w; `--exclude %s` leaves it out and bumps the rest, and the record keeps it listed so a reviewer can disagree",
				c.Port, perr, c.Port)
		}
		aux, aerr := baseFiles(ctx, p.Repo, tip, dir, pl.Files)
		if aerr != nil {
			return change.Prepared{}, aerr
		}
		part, cerr := change.Prepare(ctx, pl, change.Source{
			Base: record.Base{Sha: tip, CommittedAt: at}, Portdir: change.TreePath(dir),
			Portfile: blob, Files: aux,
		}, p.Eval)
		if cerr != nil {
			return change.Prepared{}, fmt.Errorf("%s: %w", c.Port, cerr)
		}
		parts = append(parts, part)
	}
	merged, merr := change.Merge(parts...)
	if merr != nil {
		return change.Prepared{}, merr
	}
	// THE SUBJECT IS THE CHANGE THIS COHORT IS FOR, not a member of
	// it. See cohortSummary.
	subject, serr := p.Repo.Subject(ctx, tip)
	if serr != nil {
		return change.Prepared{}, serr
	}
	if subject = strings.TrimSpace(subject); subject != "" {
		merged.Summary = cohortSummary(subject)
	}
	return merged, nil
}

// bumped is the candidates a cohort actually revbumps, in the proposal's
// order. An excluded member is still in the slice — change.Cohort marks
// rather than drops, so a reviewer can see what was left out and
// disagree — and it writes nothing.
func bumped(cands []record.Candidate) []record.Candidate {
	var out []record.Candidate
	for _, c := range cands {
		if c.Proposed && c.Portdir != "" {
			out = append(out, c)
		}
	}
	return out
}

// cohortParams is the parameters a cohort member is re-planned with: a
// revision bump for the reason the MEASUREMENT gave, which is why the
// plural road takes no --reason. Riders are RidersNone, because a cohort
// commit revbumps other people's ports and makes no other edit.
func cohortParams(dir string, cands []record.Candidate, criterion string) intent.Params {
	return intent.Params{
		Target: dir,
		Reason: cohortReason(cands, criterion),
		Riders: intent.RidersNone,
	}
}

// cohortReason is the criterion the proposal rests on, as the reason a
// revbump commit states. It is the candidate's own words and never a
// paraphrase: the whole argument for a proposal is that a person can
// check the one claim behind it by hand, and a commit body, a pull
// request and a terminal line that each reworded it would be three
// claims a reviewer has to reconcile.
func cohortReason(cands []record.Candidate, criterion string) string {
	// THE MEASUREMENT, and it was the first candidate's own Reason. Those
	// are two different sentences for two different readers: a
	// candidate's Reason says why that PORT is in the cohort
	// ("depends_lib"), and the criterion says why anybody must REBUILD
	// ("install name libcmark.0.30.3.dylib -> libcmark.0.31.2.dylib;
	// compatibility_version widened").
	//
	// Measured on the first cohort dockhand ever proposed: the commit came
	// out titled "Aseprite: depends_lib", which tells a MacPorts reviewer
	// nothing they can check — where the whole argument for a proposal is
	// that the one claim behind it can be checked by hand with otool.
	// This function's own doc already described the criterion; it read
	// the wrong field.
	if criterion != "" {
		return criterion
	}
	for _, c := range cands {
		if c.Reason != "" {
			return c.Reason
		}
	}
	return "rebuild against the headline change"
}

// stagedTarget points the planner at a member's STAGED portdir — an
// absolute path this process made — while deciding the subport from the
// record's own tree-relative name.
//
// The two are different path spaces and the split is the point.
// tree.Target.Portdir is documented "absolute portdir path"; a record's
// Portdir is tree-relative; and this function used to pass the second
// where the first was meant, which is how the evaluator came to resolve
// a portdir against the process's working directory.
func stagedTarget(staged string, c record.Candidate) tree.Target {
	t := tree.Target{Portdir: staged}
	if c.Port != "" && c.Port != pathBase(c.Portdir) {
		t.Subport = c.Port
	}
	return t
}

// pathBase is filepath-free basename over a tree-relative, slash-joined
// path: a record's Portdir is always slash-separated whatever the host.
func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func baseFiles(ctx context.Context, repo *git.Repo, sha, portdir string, files []plan.FileEdit) (map[string][]byte, error) {
	if len(files) == 0 {
		return nil, nil
	}
	batch, err := repo.CatFile(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = batch.Close() }()
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		obj, err := batch.Object(sha + ":" + portdir + "/" + f.Path)
		if errors.Is(err, git.ErrNoObject) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s at %s: %w", f.Path, git.Abbrev(sha), err)
		}
		out[f.Path] = obj.Data
	}
	return out, nil
}

// cohortSuffix is what a cohort adds to the subject of the change it is
// for. MacPorts subjects are "port: what changed", and what changed here
// is that change plus its dependents.
const cohortSuffix = ", bump dependents"

// cohortSummary is the subject line a cohort commit carries.
//
// change.Merge takes its identity from parts[0], so this used to be the
// FIRST MEMBER'S plan: "Aseprite: install name
// /opt/local/lib/libcmark.0.30.3.dylib → ... on 26.6.2 arm64". Two
// hundred and sixty-three characters, against a git convention of about
// fifty, naming a port that is in the cohort only because something else
// moved — and `promote --title` defaults to the tip's subject, so that
// was the pull request's title too.
//
// A cohort commit is stacked on the change it is for, so the tip's own
// subject IS that change's summary. "cmark: update to 0.31.2" becomes
// "cmark: update to 0.31.2, bump dependents", which names the port that
// actually moved and says what this commit adds.
//
// THE MEASUREMENT IS NOT LOST, and that matters, because the criterion
// is deliberately the candidate's own words so a reviewer can check the
// one claim behind the proposal with otool. It moves to where a reviewer
// reads it — the commit body and the pull request body — rather than
// into a title nothing can display.
//
// A branch that already carries a cohort commit keeps one suffix: a
// re-accept reads a tip that has been through here before.
func cohortSummary(tip string) string {
	if strings.HasSuffix(tip, cohortSuffix) {
		return tip
	}
	return tip + cohortSuffix
}
