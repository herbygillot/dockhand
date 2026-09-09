package change

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
)

// Simplicity is whether a change is one a machine may publish
// unattended. The zero value is Unjudged, which is a refusal.
type Simplicity uint8

const (
	Unjudged Simplicity = iota
	NotSimple
	Simple
)

// simpleKinds is the permitted set, and it is the whole of the ruling's
// edit-confinement half: a bump whose entire edit set falls in the
// version, checksum and vendored regions.
//
// What is DELIBERATELY absent, each for its own reason:
//
//   - edit.Unclassified — the zero value never permits.
//   - edit.RevisionBump — a bare revbump's whole justification is a
//     sentence a person typed, which is not a thing a machine can mean.
//   - edit.EpochBump — but note carefully that its ABSENCE here is belt
//     and braces, NOT the mechanism. Judge sees only kinds and counts, so
//     excluding a kind refuses a change only when that kind is PRESENT.
//     A downgrade whose epoch edit the planner could not emit — the
//     insertion path declines on a carrier that is not top-level, shares
//     its line, or sits in a subport block, and real downgrades do:
//     py-iniconfig rolled back one python subport, openal-soft 1.24.0 ->
//     1.23.1 inside an os.major branch — arrives here as
//     {Version, Checksum, VendoredBlock}, every one of them permitted.
//     The refusal must therefore hang on publish.Facts.Direction.EpochOwed,
//     which is computed from the realized delta and does not care whether
//     a planner managed anything. The asymmetry is what makes it matter: a
//     RevisionReset that fails to emit is cosmetic, and an EpochBump that
//     fails to emit strands every existing install.
//   - edit.ChecksumSet — placed by an aliasing heuristic over a handful of
//     ports, and the locator's own contract says the caller then owes a
//     proof that no sibling context moved.
//   - edit.VendoredNew — introduces a fetch source the Portfile did not
//     have, by a zero-width insert with no located span to contain it.
//   - edit.ToolchainMin — changes which compilers the port demands, read
//     out of a go.mod extracted from the tarball. A buildability claim,
//     not a restatement of bytes.
//   - edit.DistfileName — renames a distfile. The bytes are new AND what
//     they are called is new.
//   - edit.Rider — housekeeping is proved inert, never proved right, and
//     the ruling names three regions and a rider is none of them.
//
// Permits reports whether one edit kind is inside the ruling's confined
// set. A FUNCTION over an unexported table, because an exported map is a
// permitted set any package can widen at run time — and classify's
// eligibility survey is a second reader of this table, so the two share
// one answer rather than keeping two lists in step.
func Permits(k edit.Kind) bool { return simpleKinds[k] }

var simpleKinds = map[edit.Kind]bool{
	edit.Version:       true,
	edit.RevisionReset: true,
	edit.Checksum:      true,
	edit.VendoredBlock: true,
}

// regionWords is the ruling's own vocabulary for the regions it names,
// used ONLY to tell a person which region refused their change. Nothing
// decides from these words — the decision is Permits over the typed
// kind, which is rule 6's whole point — and a kind with no word here is
// named by its number rather than by a guess, because an unnamed kind is
// still a refusal and a refusal that could not say which region it was
// about is worse than one that says "kind 9".
var regionWords = map[edit.Kind]string{
	edit.Unclassified:  "an unclassified edit",
	edit.Version:       "the version",
	edit.RevisionReset: "the revision reset",
	edit.EpochBump:     "the epoch",
	edit.RevisionBump:  "a bare revision bump",
	edit.Checksum:      "the checksums",
	edit.ChecksumSet:   "checksums in a set carrier",
	edit.VendoredBlock: "the vendored block",
	edit.VendoredNew:   "a new vendored block",
	edit.DistfileName:  "the distfile name",
	edit.ToolchainMin:  "the toolchain minimum",
	edit.Rider:         "housekeeping",
}

func regionWord(k edit.Kind) string {
	if w, ok := regionWords[k]; ok {
		return w
	}
	return fmt.Sprintf("edit kind %d", uint8(k))
}

// Reconstruction is what re-planning a minted change from its own base
// produced, and how that compares to the tip. It exists because
// change.Judge's input could not otherwise reach the road that consumes
// it: Regions live on Prepared, in-process, at MINT — and the machine
// road is cycle's publish slot, a later process that admits on
// record.ToPublished (internal/engine/publishslot.go:342) and reads only
// the record. A draft said the verdict was "recomputed over the realized
// change" and named no mechanism. This is the mechanism.
//
// RULED 2026-09-06: reconstruction, not store-and-bind. Re-plan the
// headline subject against record.Change.Base.Sha, Prepare, Identify —
// and classify only if the tree oid it produces IS the tip's. Nothing is
// read back as permission, which is the discipline the tree states where
// AskedBy is written; and Identify promises the two oids are the same
// object BY CONSTRUCTION when the bytes are, so the comparison is exact
// rather than approximate.
//
// IT RETIRES THE STEALTH RE-WITNESS, and that is the ruling's largest
// consequence. The re-witness has exactly one caller in the whole tree —
// rewitnessBeforePush at internal/engine/publishslot.go:431, inside the
// unattended slot — so it serves this road and no other. Reconstruction
// catches the same event and more: an upstream re-roll changes the bytes,
// so re-planning writes different checksums, so the oid diverges, so the
// machine refuses. It also covers the VENDORED region, which the
// re-witness is documented fail-open on ("a re-witness that refused over
// one would hold every port that carries a block", rewitness.go:204). One
// mechanism replaces a gate that D26 showed could not see its own event.
type Reconstruction struct {
	// Compared is rule 7: false means the reconstruction could not be
	// made — no evaluator, a fetch that failed, a plan that declined —
	// which is never the same answer as "it matched".
	Compared bool
	Err      error

	Regions []record.Region  // what re-planning says the change is made of
	Content record.ContentID // the tree oid re-planning produces
	Tip     record.ContentID // the tree oid the tip actually has
	// Diverged is the portdir-relative paths whose bytes differ, and it is
	// DATA rather than a sentence because a person has to answer it: the
	// hold this raises is written with a record.Finding carrying these,
	// and a finding whose substance is prose is the defect rule 6 names.
	Diverged []string
}

// ErrUnreconstructable is the soft answer Reconstruction.Err carries
// when there was nothing to reconstruct FROM: a change with no recorded
// base, no headline portdir, or a caller with no plan or no evaluator to
// re-plan with. It is a value on the struct and not a returned error
// because "I could not compare" is an ANSWER the machine road acts on —
// Judge reads it as Unjudged, and Unjudged withholds — where a returned
// error would be an incident.
var ErrUnreconstructable = errors.New("change: the change cannot be re-planned from its own base")

// Judge answers ONE question: is this change's edit set confined to the
// permitted regions, and did the version actually move?
//
// It answers no others, which is rule 2 and is what keeps this small.
// In particular it does NOT ask whether the target is a prerelease or
// whether the version moved FORWARD, because the tree already answers
// both, earlier and better:
//
//   - Prerelease: a change minted against one is BORN HELD
//     (internal/engine/mint.go:359), and a hold is already the first
//     refusal on every publication road. The existing code's reason for
//     that hold is this ruling, written before it was made: "an
//     unattended pass that opened a pull request proposing MacPorts move
//     a port to a release candidate would be spending reviewer attention
//     on a judgment nobody made."
//   - Forward: NOT settled, and a draft of this comment said it was. It
//     cited internal/upstream/staged.go:428, where the Outdated verdict a
//     SWEEP acts on does require VerCmp(newest, current) > 0 — but the
//     sweep never writes record.ToPublished, which is what
//     internal/engine/publishslot.go:342 admits on, and which is written
//     in exactly one place: the single-target --to-pr road at
//     internal/cmd/intent.go:117. On that road direction is decided by
//     `moving := carrier.Text(src) != b.Version` (bump.go:142), a string
//     inequality, and macports.VerCmp appears nowhere in intent, engine or
//     cmd. Direction is therefore observed and gated in publish.Facts.
//
// Judge answers CONFINEMENT. Prerelease is settled by the born-held
// mechanism; direction is a separate observation, and saying it was
// settled when it was not is how a ruling's condition goes missing.
// The version must MOVE: edit.Version has to be present, not merely
// permitted. That single line is what the ruling narrowed to — a
// same-version checksum refresh is now NotSimple, because the only gate
// that could have vouched for it cannot. The stealth re-witness
// materializes the tip and compares the tip's OWN recorded checksums
// against the bytes upstream serves (internal/engine/rewitness.go:147),
// and on a refresh dockhand computed those checksums from those very
// bytes minutes earlier — so it re-checks its own arithmetic and agrees
// by construction. It catches a re-roll BETWEEN mint and publish; it is
// structurally blind to the one that MOTIVATED the refresh. It is also
// documented fail-open on the vendored region: recordedFiles skips a
// distfile a vendored block supplies, because refusing over one "would
// hold every port that carries a block" (rewitness.go:204).
// It takes the whole Reconstruction and not a bare region slice, so the
// identity comparison cannot be forgotten by a caller — the discipline
// the deleted Proof.Transfers once stated, for the same reason. A Reconstruction
// that did not compare, or whose Content is not the Tip, is Unjudged:
// never NotSimple, because "these are different bytes" is not a finding
// about simplicity.
func Judge(r Reconstruction) (Simplicity, []string) {
	if !r.Compared || r.Content == "" || r.Content != r.Tip {
		return Unjudged, nil
	}
	var why []string
	moved := false
	for _, region := range r.Regions {
		if region.Edits == 0 {
			continue
		}
		if region.Kind == edit.Version {
			moved = true
		}
		if !Permits(region.Kind) {
			why = append(why, "it edits "+regionWord(region.Kind))
		}
	}
	if len(why) > 0 {
		return NotSimple, why
	}
	if !moved {
		return NotSimple, []string{"the version does not move"}
	}
	return Simple, nil
}

// Reconstruct re-derives a minted change from its base and a fresh plan,
// so a later process can judge bytes it did not prepare. The plan comes
// from planning; change holds neither an evaluator nor a fetcher of its
// own, which is why they arrive as arguments.
//
// The returned error is an INCIDENT — git would not answer — and never a
// verdict. Everything that is merely absent (no base on the record, no
// plan, no evaluator, a plan that declines) comes back as Compared false
// with Err set, because a machine road that could not compare must
// withhold rather than stop, and Judge reads exactly that.
func Reconstruct(ctx context.Context, repo *git.Repo, c record.Change, p *plan.Plan, ev Evaluator) (Reconstruction, error) {
	if repo == nil || c.Tip == "" {
		return Reconstruction{Err: fmt.Errorf("%w: the change names no tip", ErrUnreconstructable)}, nil
	}
	if c.Base.Sha == "" {
		return Reconstruction{Err: fmt.Errorf("%w: the change records no base", ErrUnreconstructable)}, nil
	}
	if p == nil || ev == nil {
		return Reconstruction{Err: fmt.Errorf("%w: no plan and evaluator to re-plan with", ErrUnreconstructable)}, nil
	}
	if len(c.Subjects) == 0 || c.Subjects[0].Portdir == "" {
		return Reconstruction{Err: fmt.Errorf("%w: the change names no headline portdir", ErrUnreconstructable)}, nil
	}
	// The tip's tree, first, because it is what the whole comparison is
	// against and because a tip git cannot resolve is an incident rather
	// than an absence.
	tip, err := repo.RevParse(ctx, c.Tip+"^{tree}")
	if err != nil {
		return Reconstruction{}, err
	}
	out := Reconstruction{Tip: record.ContentID(tip)}
	portdir := TreePath(c.Subjects[0].Portdir)
	base, err := repo.BlobAt(ctx, c.Base.Sha, string(portdir)+"/"+macports.PortfileName)
	if err != nil {
		return Reconstruction{}, err
	}
	prepared, err := Prepare(ctx, p, Source{Base: c.Base, Portdir: portdir, Portfile: base}, ev)
	if err != nil {
		// A re-plan that will not prepare against the base — the Portfile
		// drifted, the prediction is about another state — is the honest
		// "could not compare", not a failure of this process.
		out.Err = err
		return out, nil
	}
	content, err := prepared.Identify(ctx, repo, c.Base.Sha)
	if err != nil {
		return Reconstruction{}, err
	}
	out.Compared, out.Regions, out.Content = true, prepared.Regions, content
	if content == out.Tip {
		return out, nil
	}
	if out.Diverged, err = diverged(ctx, repo, c.Tip, prepared); err != nil {
		return Reconstruction{}, err
	}
	return out, nil
}

// diverged names the portdir-relative paths where a reconstruction's
// bytes are not the tip's. It is what a person is handed when the
// machine refuses, and it is paths rather than a sentence because the
// pass that raises the hold is unattended: the only account anybody gets
// is what is written down.
//
// It compares the reconstruction's OWN files against the tip, which
// answers the question that was asked — "which of the files this change
// writes does the tip disagree with" — and deliberately not the whole
// portdir: a file the tip carries that the re-plan does not write is a
// file the change never claimed, and naming it would send a reader after
// a divergence that is not one.
//
// The read is a batch session and not BlobAt, for rule 7: BlobAt hands
// back a wrapped exit status for a path the tree does not carry and for
// a git that would not run, and a comparison that read the second as a
// divergence would name a file over a broken repository. git.ErrNoObject
// is the typed absence, and absence IS a divergence — the change claims
// to write a path the commit does not have.
func diverged(ctx context.Context, repo *git.Repo, tip string, p Prepared) ([]string, error) {
	files, err := p.materialize()
	if err != nil {
		return nil, err
	}
	batch, err := repo.CatFile(ctx)
	if err != nil {
		return nil, err
	}
	defer batch.Close() //nolint:errcheck // read-path close; nothing was written
	var out []string
	for _, f := range files {
		obj, err := batch.Object(tip + ":" + f.Path)
		switch {
		case errors.Is(err, git.ErrNoObject):
			// Absent from the tip. A file the change writes and the commit
			// does not carry has diverged; a file the change DELETES and the
			// commit does not carry agrees with it.
			if !f.Delete {
				out = append(out, f.Path)
			}
			continue
		case err != nil:
			return nil, err
		}
		if f.Delete || string(obj.Data) != string(f.Content) {
			out = append(out, f.Path)
		}
	}
	slices.Sort(out)
	return out, nil
}

// portdirRel USED TO LIVE HERE, undoing materialize's join so a
// divergence was reported by its portdir-relative name. There is no join
// to undo: change.File.Path is tree-relative now, and a tree-relative
// name is the better one to report anyway — a change may span portdirs,
// and "Portfile" would name several of them.
