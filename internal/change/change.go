// Package change owns the change lifecycle: a prepared edit becomes a
// branch, a branch is extended, superseded or discarded. It is the one
// place that writes a file a change carries and the one place that
// mints a commit.
package change

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
)

// TreePath is a path relative to the root of the git tree, slash
// separated, the way a tree names it — and it exists because the build
// order named this step as the one where "Plan.Portdir's
// absolute-versus-relative ambiguity has to be settled, and it is
// settled by the field's type, not by a convention".
//
// The two values are genuinely different things. plan.Plan.Portdir is a
// HOST path: plan.Apply joins the Portfile name onto it and calls
// os.ReadFile, so an in-place realization needs the absolute directory a
// person's tree actually has. What a commit needs is the path inside the
// tree, and git.File.Path is documented "slash-separated, repo-relative"
// — a leading slash is an empty first segment, which GraftTree refuses.
// A change built from the first would refuse at the graft, or, if some
// future graft were more forgiving, write a portdir nobody asked for.
//
// So the conversion happens once, at the boundary, in the caller that
// holds a repository (git.Repo.RelPath), and from there on the type says
// which of the two a value is. Nothing in this package accepts a host
// path, and nothing hands one to git.
type TreePath string

// File is one file a change writes. Mode and Delete are here because
// the current design has no way to express either, and a change that
// can only rewrite an existing text file is a change that cannot
// remove a stale patch.
type File struct {
	Path    string // portdir-relative, slash-separated
	Mode    fs.FileMode
	Content []byte
	Delete  bool
}

// ErrMode is the refusal of a File asking for a mode the object writer
// cannot yet give it, and it is a REFUSAL rather than a silent
// downgrade because a change that says 0755 and lands 0644 is a change
// that lied about what it wrote.
//
// git.File carries no mode: GraftTree writes a path git has not seen at
// "100644" and leaves a path already in the tree at whatever mode the
// tree gives it, which is exactly the two behaviours Mode exists to
// stop being the only ones available. Until the tree writer carries a
// mode, the modes this package can honestly promise are the zero value
// — "whatever the tree already says, or 0644 for a new path" — and
// 0o644 itself, which is that same answer said out loud. Anything else
// is refused here, and the day GraftTree takes a mode this refusal is
// deleted rather than relaxed.
var ErrMode = errors.New("change: a file mode other than the tree's own cannot be written yet")

// Prepared is the complete content of one change: every file it
// writes, the semantic delta it predicts, and a digest over the whole
// set. It is the single owner of the file set that today is written by
// four separate loops in gate.go, mint.go, run.go and cohort.go, none
// of which is the owner and none of which knows about the others.
type Prepared struct {
	Portdir  TreePath
	Subjects []record.Subject
	Files    []File
	Predict  info.Delta
	// Before and After are the EVALUATED values of the headline port on
	// either side of the change — info.Values.Version, never the carrier
	// literal — and they are here because record.Crossing needs a producer
	// and the build order named this step as the one that gives it one:
	// Prepare is the only function holding both evaluations. Cross reads
	// them; nothing else does.
	Before info.Values
	After  info.Values
	// Base is the commit the plan was made against, carried so Commit's
	// parent and MintIn's Base are the SAME value the drift check
	// (ErrDrift) compares — a draft had Commit take a base the caller
	// re-read, which is the two-moments-one-value shape rule 2 forbids.
	Base     record.Base
	Intent   string
	Summary  string
	Closes   string
	Findings []plan.Finding
	// Regions is what this change's edits were made of, recorded here
	// because Prepare is the only function holding both the plans and the
	// content they produce.
	Regions []record.Region
}

// materialize is THE loop, and its being one function is the whole of
// the collapse this step was named for: gate.go, mint.go, run.go and
// cohort.go each walked a plan's files into a git.File list, none of
// them owned the walk, and none of them knew the others existed — so a
// path prefix fixed in one stayed wrong in three. Every road that turns
// a change's content into objects comes through here: Identify, and
// therefore Commit, and therefore Snapshot.
//
// The portdir prefix is joined here and nowhere else, which is what
// makes TreePath's promise reach git: a File's Path is portdir-relative
// by contract, the portdir is tree-relative by type, and the sum is what
// git.File is documented to want. GraftTree refuses an empty segment, so
// a Path that is not what it claims fails at the tree rather than
// landing somewhere else in it.
func (p Prepared) materialize() ([]git.File, error) {
	files := make([]git.File, 0, len(p.Files))
	for _, f := range p.Files {
		if f.Mode != 0 && f.Mode != 0o644 {
			return nil, fmt.Errorf("%w: %s asks for %v", ErrMode, f.Path, f.Mode)
		}
		files = append(files, git.File{
			Path:    string(p.Portdir) + "/" + f.Path,
			Content: f.Content,
			Delete:  f.Delete,
		})
	}
	return files, nil
}

// Identify is the content identity of a prepared set over a base: the
// git tree object id the change produces. GraftTree already computes
// it — Repo.commit is literally GraftTree followed by commit-tree — so
// the tree a pre-mint gate builds and the tree a mint commits are the
// same object id BY CONSTRUCTION, with no second hashing scheme to get
// wrong and nothing to keep in step.
//
// It takes a base because the same edits over a different base are a
// different change, which a digest over the written files alone would
// not catch. It writes tree objects and no commit and no ref, so a
// gate can name what it verified without minting anything; unreferenced
// trees are collectable.
//
// record.Record already carries this value under the name Tree. What
// changes is that a run and a proof carry it too.
func (p Prepared) Identify(ctx context.Context, repo *git.Repo, base string) (record.ContentID, error) {
	files, err := p.materialize()
	if err != nil {
		return "", err
	}
	oid, err := repo.GraftTree(ctx, base, files)
	return record.ContentID(oid), err
}

var ErrDrift = errors.New("change: the base is not the one planned against")

// ErrPredicted is Prepare refusing a plan whose prediction is not about
// the port as it evaluates now: the plan says the version moves FROM one
// string and the evaluator says the port is at another.
//
// It is a separate refusal from ErrDrift, which is the Portfile's BYTES
// having moved under the plan (plan.Materialize's precondition hash).
// This is the case where the bytes are the ones planned against and the
// evaluated meaning is not — a PortGroup underneath the port changed, a
// variable the version is composed from moved — and a crossing computed
// over the two sides would be comparing one port's before with another's
// after.
var ErrPredicted = errors.New("change: the plan's prediction does not describe the port as it evaluates")

// Regions are record.Region — this package declares no Region of its own,
// and a draft of it did. change already imports record, the two structs
// were field-for-field identical one import apart, and every publication
// write would have needed a conversion that existed only because the type
// was declared twice. That is the same-name collision this document names
// as its central theme, committed inside it for the FOURTH time after
// run.Request/verify.Request, publish.Step/record.Step, and statestore's
// second Lease. Four occurrences is not carelessness; it is evidence the
// rule needs a check rather than a paragraph.

// Evaluator is the narrow slice of the Tcl evaluator that preparation
// needs: read a portdir's evaluated values. It is declared here, by
// the consumer, because preparation must be substitutable in a test
// without a MacPorts installation.
type Evaluator interface {
	Values(ctx context.Context, portdir string) (info.Values, error)
}

// Source is where a preparation reads from, and it is a struct rather
// than three parameters because two of the three are values the sketch's
// own signature could not supply and Prepared cannot do without.
//
// Base is the first. Prepared.Base is documented as the value Commit's
// parent and MintIn's Base BOTH come from — the point being that there
// is one of it, established once — and a Prepare that took only the
// Portfile's bytes would leave the field for a caller to fill from a
// second read, which is the two-moments-one-value shape the field exists
// to prevent.
//
// Portdir is the second, and it is TreePath for the reason TreePath
// exists: the caller had to compute the tree-relative portdir already,
// because that is what it read Portfile out of the base commit BY, and
// asking Prepare to derive it again would need a repository this
// function deliberately does not take.
//
// Portfile is the base commit's bytes at <Portdir>/Portfile — the blob,
// never the working file. plan.Materialize holds them against the plan's
// precondition hash, so a Portfile a person edited on the primary branch
// since the plan was made is ErrDrift here rather than a commit nobody
// predicted.
type Source struct {
	Base     record.Base
	Portdir  TreePath
	Portfile []byte
}

// Prepare turns a plan into a complete file set against a base blob,
// proving the predicted delta before returning. This is plan.Apply and
// plan.Materialize with the auxiliary files folded in, which is the
// whole of the fix: there is no longer a second place to write a file.
//
// WHAT "PROVING" IS HERE, precisely, because the word is load-bearing
// and the double proof plan.Apply performs is not available to this
// function. plan.Apply writes the edited Portfile into the tree,
// re-evaluates, and demands the observed delta equal the predicted one;
// that is the PLANNER's discipline, spent at plan time with a shadow
// directory in hand (the planning layer holds a tempdir.Root for exactly
// it), and re-spending it here would need this package to hold a
// temporary tree and a second evaluation of bytes it has just written.
// What Prepare can hold against the world, it does: the plan's
// precondition hash against the base blob (ErrDrift), and the plan's
// predicted FROM version against what the evaluator says the port is at
// now (ErrPredicted). Both are refusals of a plan that is about some
// other state of this port.
//
// The evaluation is the one that makes Before and After real. A nil
// Evaluator is a caller with no MacPorts installation to ask, and it
// leaves both sides zero — which Cross answers CrossingUnknown and
// Crossing.WithholdsUnattended holds (rule 7). An evaluator that FAILS is
// not that case and is returned as itself: the planner needed an
// evaluation to make this plan at all, so a failure here is a fault and
// not an absence, and rule 7's slot is for the caller who never asked.
func Prepare(ctx context.Context, p *plan.Plan, src Source, ev Evaluator) (Prepared, error) {
	if p == nil {
		return Prepared{}, fmt.Errorf("%w: no plan", ErrIncomplete)
	}
	if src.Portdir == "" || src.Base.Sha == "" {
		return Prepared{}, fmt.Errorf("%w: a preparation needs a base commit and a tree-relative portdir", ErrIncomplete)
	}
	edited, err := p.Materialize(src.Portfile)
	if errors.Is(err, plan.ErrDrift) {
		return Prepared{}, fmt.Errorf("%w: %s at %s", ErrDrift, macports.PortfileName, git.Abbrev(src.Base.Sha))
	}
	if err != nil {
		return Prepared{}, err
	}
	// The Portfile first and the plan's whole files after it, in the
	// plan's own order: one list, written once, and the only list any
	// realization of this change is built from.
	files := make([]File, 0, 1+len(p.Files))
	files = append(files, File{Path: macports.PortfileName, Content: edited})
	for _, f := range p.Files {
		files = append(files, File{Path: f.Path, Content: []byte(f.Content)})
	}
	out := Prepared{
		Portdir: src.Portdir,
		Subjects: []record.Subject{{
			Port: p.Port,
			// Written as [Port] and not left empty. The empty slice already
			// means something else — nobody asked — and the planner did ask:
			// it evaluated the Portfile and named the one context this change
			// moves.
			Names:   []string{p.Port},
			Portdir: string(src.Portdir),
			Intent:  p.Intent,
			Target:  targetIn(p.Slug, p.Port),
		}},
		Files:    files,
		Predict:  predictOf(p),
		Base:     src.Base,
		Intent:   p.Intent,
		Summary:  p.Summary,
		Closes:   p.ClosesTicket,
		Findings: p.Findings,
		Regions:  regionsOf(p.Edits),
	}
	if ev == nil {
		return out, nil
	}
	before, err := ev.Values(ctx, p.Portdir)
	if err != nil {
		return Prepared{}, err
	}
	from, to, moves := predictedVersion(p)
	if moves && from != "" && before.Version != from {
		return Prepared{}, fmt.Errorf("%w: it moves %s from %q and the port evaluates to %q",
			ErrPredicted, p.Port, from, before.Version)
	}
	out.Before, out.After = before, before
	if moves {
		out.After.Version = to
	}
	return out, nil
}

// targetIn is what a change moves its port to — "1.9", "checksums",
// "rev2" — recovered from the two values the planner already holds.
//
// It is not the branch-name reading that record.Subject.Target exists to
// end. That one has a name and nothing else, and must split it
// somewhere; a slug and the port that built it are both here, so cutting
// the one off the other is inverting a construction rather than parsing
// a string. A slug that does not carry the port keeps nothing: a wrong
// target is worse than an absent one.
func targetIn(slug, port string) string {
	if port == "" {
		return ""
	}
	rest, ok := strings.CutPrefix(slug, port+"-")
	if !ok {
		return ""
	}
	return rest
}

// regionsOf is what a change is MADE OF, counted by typed kind: the
// durable basis Judge weighs and record.Publication.Basis keeps. It
// counts rather than lists because the question the ruling asks is which
// regions were touched and how much, never which byte spans — and a
// basis that carried spans would be a diff written twice.
//
// Sorted by kind so two preparations of the same edit set produce the
// same slice, which is what lets a reconstruction's regions be compared
// with a mint's at all.
func regionsOf(edits []edit.Edit) []record.Region {
	counts := map[edit.Kind]int{}
	for _, e := range edits {
		counts[e.Kind]++
	}
	kinds := make([]edit.Kind, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	regions := make([]record.Region, 0, len(kinds))
	for _, k := range kinds {
		regions = append(regions, record.Region{Kind: k, Edits: counts[k]})
	}
	return regions
}

// predictedVersion is the version movement the plan predicts for the
// context being changed: the old string, the new one, and whether the
// plan predicts a version movement at all.
//
// It reads the plan's own canonical wire form rather than re-deriving
// anything. The context is the plan's subport where it names one and the
// port otherwise, under the empty variant set — the frame every planner
// shadows in — and a plan that predicts exactly one context is taken at
// its word whatever that context is called, because a subport whose
// evaluated name differs from the port's is a real shape (devel/pcre
// carries pcre2) and the alternative is reporting no movement for it.
func predictedVersion(p *plan.Plan) (from, to string, ok bool) {
	want := p.Subport
	if want == "" {
		want = p.Port
	}
	var only *plan.ContextDelta
	for i := range p.Predicted {
		cd := &p.Predicted[i]
		if cd.Added || cd.Removed || cd.Variants != "" {
			continue
		}
		if cd.Subport == want {
			only = cd
			break
		}
		if only == nil {
			only = cd
			continue
		}
		// A second candidate context: no single one is "the" context, so
		// the named one is the only answer, and there is none.
		only = nil
		break
	}
	if only == nil {
		return "", "", false
	}
	for _, ch := range only.Changes {
		if ch.Field != info.FieldVersion.String() {
			continue
		}
		return first(ch.Old), first(ch.New), true
	}
	return "", "", false
}

// first is the scalar behind info's uniform []string representation: one
// element, or "" for a field the evaluation did not have.
func first(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// predictOf lifts the plan's canonical wire form back into an
// info.Delta, which is the shape Prepared carries because a delta is
// what a reader of a change asks for and plan.ContextDelta is a
// rendering of one.
//
// WHAT SURVIVES THE ROUND TRIP AND WHAT DOES NOT, said here rather than
// discovered later. A field change survives whole: the field name is
// info.Field's own String, so the inversion is over one vocabulary
// rather than two (rule 6 — nothing here recovers a fact by reading a
// word somebody else chose). A context that APPEARED or VANISHED does
// not: plan's wire form renders such a context as one-sided changes and
// keeps no info.Values to re-inflate, and info exports no way to build
// one field by field. The KEY survives, and the key is what the only
// consumer in the tree reads — info.Delta.OtherContext asks which
// contexts a delta touches, in any way, and never their values — so the
// loss is stated and bounded rather than silent.
func predictOf(p *plan.Plan) info.Delta {
	var d info.Delta
	for _, cd := range p.Predicted {
		key := info.SubportKey{Subport: cd.Subport, Variants: info.VariantSet(cd.Variants)}
		switch {
		case cd.Added:
			if d.Added == nil {
				d.Added = map[info.SubportKey]info.Values{}
			}
			d.Added[key] = info.Values{}
		case cd.Removed:
			if d.Removed == nil {
				d.Removed = map[info.SubportKey]info.Values{}
			}
			d.Removed[key] = info.Values{}
		default:
			changes := make([]info.FieldChange, 0, len(cd.Changes))
			for _, ch := range cd.Changes {
				f, ok := fieldNamed(ch.Field)
				if !ok {
					continue
				}
				changes = append(changes, info.FieldChange{Field: f, Old: ch.Old, New: ch.New})
			}
			if len(changes) == 0 {
				continue
			}
			if d.Changed == nil {
				d.Changed = map[info.SubportKey][]info.FieldChange{}
			}
			d.Changed[key] = changes
		}
	}
	return d
}

// fieldNamed inverts info.Field.String over the closed set. The bounds
// are the set's own first and last constants rather than a copy of the
// list, so a field added to info is inverted here without an edit and a
// field RENAMED there stops matching here loudly rather than quietly:
// the name comes from one place either way.
func fieldNamed(name string) (info.Field, bool) {
	for f := info.FieldName; f <= info.FieldDependsTest; f++ {
		if f.String() == name {
			return f, true
		}
	}
	return 0, false
}

// Cross is the mint-time stability fact record.Crossing is the durable
// form of, computed over the EVALUATED versions Predict carries — never
// the carrier literal, for the reason record.Crossing's doc spells out
// with three real ports. It is a pure function here and not in app,
// because a crossing is a fact about the change: app calls it and passes
// the value into MintIn, which is what "a dependency is a value passed
// in" means for a fact rather than a service. Where either side is
// unavailable it answers CrossingUnknown, which WithholdsUnattended holds
// (rule 7), and it never refuses: the human road warns, the machine road
// withholds, and both read the one value.
func Cross(p Prepared) record.Crossing {
	from, to := p.Before.Version, p.After.Version
	if from == "" || to == "" {
		return record.CrossingUnknown
	}
	switch fs, ts := !macports.Prerelease(from), !macports.Prerelease(to); {
	case fs && ts:
		return record.StableToStable
	case fs && !ts:
		return record.StableToPrerelease
	case !fs && ts:
		return record.PrereleaseToStable
	default:
		return record.PrereleaseLateral
	}
}

// tracTicket is where a MacPorts ticket lives, spelled once because the
// commit trailer and the pull request body write the same URL from the
// same number, and two spellings of it are two.
const tracTicket = "https://trac.macports.org/ticket/"

// Message builds the commit message for a prepared set: the summary, the
// body, and Closes as a TRAILER — the one place the ticket is spelled
// into git, from which PromoteFacts reads it back off the record and
// never off the message (R16: named once on the bump and carried). The
// cohort form (Accept) takes the same function with the cohort's
// subjects, which is what retires render/cohort.go's CohortMessage as a
// second message-writer.
//
// The subject is the plan's Summary, which is the plan's identity: a
// realizer that composed its own subject would be re-deciding what the
// change is called. What the realizer adds is what is NOT a subject —
// the members a cohort carries, and the trailer — because a plan's bytes
// are a hash gate and a summary that grew a paragraph would be a
// different plan for the same change.
//
// The trailer is LAST and after a blank line, because git reads a
// trailer only in the last paragraph and a subject line is a paragraph.
// No trailing newline: git supplies one, and two messages differing by
// one are two different commits (git.Repo.CommitTree passes the bytes
// through verbatim).
func Message(p Prepared) string {
	var b strings.Builder
	b.WriteString(p.Summary)
	if members := p.Subjects; len(members) > 1 {
		// A cohort's body says why all N moved. The headline is
		// Subjects[0] and has its own subject line; the rest are the
		// members, each with the reason the proposal gave for it, which is
		// the sentence a reviewer checks.
		b.WriteString("\n\nRevision bumped in this change:\n")
		for _, m := range members[1:] {
			b.WriteString("  " + memberLine(m) + "\n")
		}
	}
	if p.Closes != "" {
		fmt.Fprintf(&b, "\n\nCloses: %s%s", tracTicket, p.Closes)
	}
	return b.String()
}

// memberLine is one cohort member as the commit body states it: the
// port, where it lives, and why it is here. The reason is written out
// verbatim rather than reworded, which is the whole point of a criterion
// being made once in the judgment: a commit body, a pull request and a
// terminal line that each paraphrased it would be three claims a
// reviewer has to reconcile.
func memberLine(m record.Subject) string {
	line := m.Port
	if m.Portdir != "" {
		line += " (" + m.Portdir + ")"
	}
	if m.Reason != "" {
		line += ": " + m.Reason
	}
	return line
}

// Commit writes the prepared set as an UNREFERENCED commit object over
// base — GraftTree for the tree, CommitTree for the commit — and returns
// the sha and the tree oid. It is a PURE OBJECT WRITER: no ref is
// created, moved or asserted here, and none is anywhere outside
// statestore.Amend's batch, which is R23. The object is named by the
// batch that writes the record for it, or it is not named at all; a
// process that dies between here and that batch leaves an unreferenced
// object, and an unreferenced object is garbage — not a recorded state,
// not a recovery case, not a stray any pass sweeps — collected under
// statestore.PruneExpire's window. Stated once, there; every object
// writer in this package points at it. The shipped Mint moved the ref
// inside the call that committed, so a crash after it left a branch
// with no record — the kind cycle had to adopt by enumeration.
//
// The tree oid it returns IS the ContentID, by construction, and it is
// the same oid Identify computes: Commit is Identify followed by
// CommitTree, so a record's Content and its Tip's tree can never
// disagree.
func Commit(ctx context.Context, repo *git.Repo, p Prepared, base string) (sha string, content record.ContentID, err error) {
	if repo == nil || base == "" {
		return "", "", fmt.Errorf("%w: a commit needs a repository and a base", ErrIncomplete)
	}
	content, err = p.Identify(ctx, repo, base)
	if err != nil {
		return "", "", err
	}
	parent, err := repo.RevParse(ctx, base+"^{commit}")
	if err != nil {
		return "", "", err
	}
	sha, err = repo.CommitTree(ctx, string(content), []string{parent}, Message(p))
	if err != nil {
		return "", "", err
	}
	return sha, content, nil
}

// BranchRef is the full ref name of a dockhand branch. It lives here and
// PinRef beside it because these two functions are the ONLY places the
// design spells a ref literal outside statestore — check_callers.py
// counts, and internal/statestore/onlymover_test.go re-proves it on
// every build — so a road that wants to name a ref has to come through
// the owner of the lifecycle whose ref it is.
func BranchRef(branch string) string { return "refs/heads/" + branch }

// PinRef is where a branchless snapshot's commit is kept alive:
// refs/dockhand/verify/<change id>. A working tree has no ref keeping
// its tree object reachable and no plan a drain could re-derive, so the
// first version of the working-tree road queued an attempt over an oid
// that git could collect and a dispatcher could not materialize — "60
// queued" over nothing buildable. The pin makes the record
// SELF-SUFFICIENT: the attempt's Sha is a real commit, and the drain
// stages it exactly as it stages a branch tip, so the re-plan road is
// ONE road and a snapshot is merely a sha nobody named a branch for. It is
// CREATED by AdoptIn's line and DELETED by CloseIn's, both inside the
// batch that writes the record they belong to; nothing else names it,
// and record.Change.Pin is the record's own statement that it exists.
func PinRef(id record.ChangeID) string { return "refs/dockhand/verify/" + string(id) }

// Provenance is who asked and how the change came to exist. Destination
// is NOT here any more — it is on Minting, because the same content
// minted by a person and by a cron are the same content, and where a
// change is bound for is a fact about one minting rather than about its
// author.
type Provenance struct {
	AskedBy record.Driver
	Agent   string
	Via     record.MintedVia
}
