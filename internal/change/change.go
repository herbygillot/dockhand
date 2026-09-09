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
//
// PATH IS TREE-RELATIVE, and it was portdir-relative. The two are one
// join apart and the join is the whole of the difference: while every
// File in a set shared one prefix, Prepared.materialize could supply it
// — and a set whose files did NOT share one prefix could not be
// expressed at all. That is a cohort. A port's dependents live wherever
// they live: cmark's six span graphics, games, multimedia and net, and
// no single prefix reaches them.
//
// So the prefix moves to the producers, which is where TreePath's own
// doc says a conversion belongs — "once, at the boundary, in the caller
// that holds a repository" — and this field now means what git.File.Path
// already means, "slash-separated, repo-relative". materialize passes it
// through rather than joining, and a Prepared becomes something several
// of which can be assembled into one change (see Merge).
type File struct {
	Path    string // tree-relative, slash-separated
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
			Path:    f.Path,
			Content: f.Content,
			Delete:  f.Delete,
		})
	}
	return files, nil
}

// precondition holds one whole-file edit against the base, which is the
// drift check the Portfile has always had and the files beside it never
// did.
//
// TWO REFUSALS, and they are the same refusal: the plan is about some
// other state of this portdir. A rewrite whose recorded Was does not
// match the base's bytes was derived from bytes that are gone — the
// patch someone refreshed upstream while the plan sat in a file. A
// creation (an empty Was, which is the planner saying it found no such
// file) over a path the base already holds is the same mistake read from
// the other side, and it is what a producer that simply forgot to record
// Was runs into on its first real portdir. Neither writes.
//
// The message names the PATH, because a person meeting drift needs to
// know which file to look at, and the Portfile's own ErrDrift line names
// the Portfile for the same reason.
func precondition(f plan.FileEdit, src Source) error {
	was, held := src.Files[f.Path]
	switch {
	case f.Was == "" && held:
		return fmt.Errorf("%w: %s already exists at %s, and the plan was made without it",
			ErrDrift, f.Path, git.Abbrev(src.Base.Sha))
	case f.Was == "":
		return nil
	case !held:
		return fmt.Errorf("%w: %s is gone at %s, and the plan rewrites it",
			ErrDrift, f.Path, git.Abbrev(src.Base.Sha))
	case edit.FileSHA256(was) != f.Was:
		return fmt.Errorf("%w: %s at %s", ErrDrift, f.Path, git.Abbrev(src.Base.Sha))
	}
	return nil
}

// Merge assembles several prepared units into ONE change: a port's own
// change and the changes its dependents need, which is what a cohort is.
//
// IT EXISTS BECAUSE Prepare IS SINGULAR AND CORRECT. One plan makes one
// subject in one portdir with one file set, and that is exactly a bump —
// its subports share the portdir, and a selector bumping four hundred
// ports makes four hundred separate changes. What a cohort adds is not a
// wider Prepare but an assembly, and saying so keeps every member on the
// same road a solo revbump takes: the same drift check, the same
// precondition, the same evaluation, prepared by the same function.
//
// The cohort road did not assemble; it planned candidates[0] and stopped.
// So a six-member cohort bumped one port, and the refusal a person met
// ("this cohort spans 6 portdirs and change.Prepared carries one")
// described a symptom of that rather than the limit itself — two members
// sharing one portdir would have fared no better.
//
// IDENTITY COMES FROM THE FIRST and content from all of them. Portdir,
// Intent, Summary, Closes, Base, Predict, Before and After are the
// HEADLINE's — a change is one commit with one message about one thing —
// while Subjects, Files, Findings and Regions are every member's, in the
// order given. That split is the whole design: identity is about what
// the change IS, content is about what it WRITES, and only the second is
// plural.
//
// TWO REFUSALS, both about assembling things that are not one change. A
// member prepared against a different base is a member planned against a
// different tree, and the commit would carry bytes nobody predicted. Two
// members writing one path is a collision that GraftTree would refuse
// later and less clearly — "named twice in one tree" — with nothing
// saying which members disagreed.
func Merge(parts ...Prepared) (Prepared, error) {
	if len(parts) == 0 {
		return Prepared{}, fmt.Errorf("%w: nothing to assemble", ErrIncomplete)
	}
	out := parts[0]
	out.Subjects = slices.Clone(parts[0].Subjects)
	out.Files = slices.Clone(parts[0].Files)
	out.Findings = slices.Clone(parts[0].Findings)
	out.Regions = slices.Clone(parts[0].Regions)

	seen := make(map[string]string, len(out.Files))
	for _, f := range out.Files {
		seen[f.Path] = subjectName(parts[0])
	}
	for _, p := range parts[1:] {
		if p.Base.Sha != out.Base.Sha {
			return Prepared{}, fmt.Errorf("%w: %s was prepared against %s and %s against %s",
				ErrIncomplete, subjectName(p), git.Abbrev(p.Base.Sha),
				subjectName(parts[0]), git.Abbrev(out.Base.Sha))
		}
		for _, f := range p.Files {
			if by, clash := seen[f.Path]; clash {
				return Prepared{}, fmt.Errorf("%w: %s and %s both write %s",
					ErrIncomplete, by, subjectName(p), f.Path)
			}
			seen[f.Path] = subjectName(p)
			out.Files = append(out.Files, f)
		}
		out.Subjects = append(out.Subjects, p.Subjects...)
		out.Findings = append(out.Findings, p.Findings...)
		out.Regions = append(out.Regions, p.Regions...)
	}
	return out, nil
}

// subjectName is a prepared unit's port, for the sentences above. A unit
// with no subject is named by its portdir, which is the next most useful
// thing a person can look at.
func subjectName(p Prepared) string {
	if len(p.Subjects) > 0 && p.Subjects[0].Port != "" {
		return p.Subjects[0].Port
	}
	return string(p.Portdir)
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
	// Files are the BASE's bytes for the whole files the plan rewrites,
	// keyed by the same portdir-relative path plan.FileEdit carries. A
	// path the plan names and this map does not hold did not exist at the
	// base.
	//
	// It is the auxiliary half of Portfile above, and it exists because
	// the plan's one precondition used to be the Portfile's hash alone: a
	// change whose Portfile was untouched at the base but whose patch
	// file somebody had rewritten in between committed the planner's
	// stale relocation over the newer file and passed every drift check.
	//
	// The caller reads them, because reading a blob at a commit is git's
	// and this package holds no repository. An absent key is a claim —
	// "the base does not have this file" — so a caller that forgets to
	// fill it makes every rewrite look like a creation, which Prepare
	// refuses out loud rather than writing quietly.
	Files map[string][]byte
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
	// The portdir prefix is joined HERE, at the boundary that holds it,
	// and the plan's own paths stay portdir-relative on the plan (which is
	// what precondition looks them up by). See File.Path.
	under := func(rel string) string { return string(src.Portdir) + "/" + rel }
	files := make([]File, 0, 1+len(p.Files))
	files = append(files, File{Path: under(macports.PortfileName), Content: edited})
	for _, f := range p.Files {
		if err := precondition(f, src); err != nil {
			return Prepared{}, err
		}
		files = append(files, File{Path: under(f.Path), Content: []byte(f.Content)})
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

// fieldNamed inverts info.Field.String over the closed set. The set
// itself is walked — info.Fields(), which info generates from
// info.Semantic — rather than a copy of the list or a pair of bounds,
// so a field added there is inverted here without an edit and a field
// RENAMED there stops matching here loudly rather than quietly: the
// name comes from one place either way.
//
// It walked the constants FieldName..FieldDependsTest until the
// comparison table became generated. That was a hole of exactly the
// kind the generation closed: a field appended AFTER the last constant
// named here would have been compared by info, recorded in a plan, and
// then silently dropped on the way back in — the loop would never have
// reached it. Asking the set for its members has no last constant to
// go stale.
func fieldNamed(name string) (info.Field, bool) {
	for _, f := range info.Fields() {
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

// listed is the members a commit body names.
//
// The subject line names the headline in the project's "port: what
// changed" convention, so repeating it in the body would say the same
// thing twice. That is why this skipped Subjects[0], and for a bump it
// is still right.
//
// A COHORT HAS NO HEADLINE MEMBER. Its subject is the change the cohort
// is FOR — "cmark: update to 0.31.2, bump dependents" — and cmark is not
// one of the ports it revbumps at all; Subjects[0] is merely whichever
// member merged first. Skipping it dropped a Portfile the commit
// actually edits. Measured on cmark's cohort: the commit body listed
// four of the five ports it changed, while the pull request body next
// door listed all five, which is how the two renderers disagreeing
// showed which one was wrong.
//
// So the test is the convention itself, and it degrades safely either
// way: skip the first subject when the subject line already names it,
// list it when it does not.
func listed(p Prepared) []record.Subject {
	if len(p.Subjects) > 0 && strings.HasPrefix(p.Summary, p.Subjects[0].Port+":") {
		return p.Subjects[1:]
	}
	return p.Subjects
}

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
	if members := listed(p); len(members) > 0 && len(p.Subjects) > 1 {
		// A cohort's body says why all N moved, each with the reason the
		// proposal gave for it, which is the sentence a reviewer checks.
		b.WriteString("\n\nRevision bumped in this change:\n")
		for _, m := range members {
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
