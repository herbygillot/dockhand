package engine

import (
	"context"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// Publication is one act of putting a change in front of reviewers:
// which tip went out, under which branch, with which pull request, and
// on what evidence.
type Publication struct {
	// MintSha is the commit that was published. It is the key the audit
	// gathers a change's rows under, so it is the tip as it stood when
	// the push happened and not whatever the branch points at later.
	MintSha string
	Branch  string
	// Port is the headline subject's, which names the subport the
	// verification was about. The portdir's base name is a guess and is
	// not accepted here in its place: an empty port is recorded as empty.
	Port string
	// Target is what the change moves that port to, as the record kept
	// it — the value the planner named, rather than a reading of the
	// branch name. Empty for a record minted before subjects carried
	// one, or by something other than a mint, where the reading below is
	// still the best answer available.
	Target string
	// PRNumber is the pull request the publication opened or updated,
	// zero when the change was pushed with no PR asked for.
	PRNumber int
	// Verified is whether the tip's verdict set cleared the gate at the
	// moment of publication. It is recorded rather than re-derived later
	// because the record keeps gaining runs after review starts, and the
	// audit's question is what was known when the change went out.
	Verified bool
	// Invoker is who performed the publication: a person at promote, and
	// the machine at the reconciler's own slot, which is the caller this
	// field was reserved for and now has.
	//
	// PROVENANCE ONLY once it is written down. Nothing reads it back to
	// decide what a road may do — the gates take an invoker as a
	// parameter at their own call sites — because a change that could
	// authorize itself by claiming its own history is not gated at all.
	Invoker record.Driver
	// AskedBy is who asked for the CHANGE, as its own record remembers,
	// which is not always who published it. The shape the slot exists to
	// produce is a person's change an unattended pass puts out, and a row
	// claiming the machine asked for it is the exact inverse of the
	// provenance the trust ladder's numerator counts.
	//
	// The caller supplies it, from the record it is already holding, so
	// that Publish still reads no record: provenance travels into this
	// file as a value for the same reason the invoker does. Empty falls
	// back to the publisher, which is the right answer for a change minted
	// before the field existed and the only answer available for one
	// dockhand did not mint.
	AskedBy record.Driver
	// Unproven is how many members were published without a pass.
	Unproven int
}

// Publish records a publication in the audit log — the opening row of
// what became of one change, keyed by the mint sha, on the ref discard
// leaves standing.
//
// It is here rather than in either publisher because there will be two
// of them: the human path promote drives, and the machine path the
// reconciler will. A row written in one place for whichever asked is
// what makes "how did this change reach review" a query rather than an
// estimate, and it is the reason the invoker is a parameter instead of
// something inferred from which code path arrived.
//
// It says nothing on any stream. The audit is a side record of a verb
// that has already said its piece, and a publication that printed twice
// would be a change to what dockhand tells the user for the sake of
// bookkeeping the user did not ask about.
//
// The error is returned rather than swallowed, and a caller with a pull
// request already open is expected to warn on it rather than fail: by
// the time this runs the change is public, and reporting a successful
// promotion as a failure because a note could not be appended would be
// the more misleading of the two.
func (e *Engine) Publish(ctx context.Context, repo *git.Repo, p Publication) error {
	evidence := record.Unverified
	if p.Verified {
		evidence = record.Verified
	}
	return e.Ledger(repo).Outcome(ctx, record.OutcomeRow{
		MintSha:  p.MintSha,
		Branch:   p.Branch,
		Port:     p.Port,
		Unproven: p.Unproven,
		Target:   targetOr(p.Target, p.Branch, p.Port),
		// Every mint today has exactly one named target — the intent road
		// refuses a second — so there is nothing that could make this
		// anything else, and writing it as a constant is the honest way to
		// say that. When a sweep exists, where a branch came from will be
		// on its record and read from there, not assumed here.
		MintedVia: record.MintedSingle,
		// The two halves of provenance, parted. They are separate fields on
		// the wire because they separate in practice — the slot publishes
		// what somebody else queued, so a human-asked, machine-published row
		// is the shape the trust ladder's arithmetic counts — and the
		// parting is made by the CALLER: it hands in what the record
		// remembers, so this package still reads no record and the
		// provenance rule stays where it was drawn.
		AskedBy:     askedOr(p.AskedBy, p.Invoker),
		PublishedBy: p.Invoker,
		Evidence:    evidence,
		PRNumber:    p.PRNumber,
		PublishedAt: stamp(time.Now()),
	})
}

// askedOr falls back to the publisher when the change's own record does
// not say who asked.
//
// A row that named nobody would be a hole in the one log the ladder's
// arithmetic is computed from, and there is exactly one honest guess to
// make: on a change dockhand did not mint, or one minted before the
// field existed, whoever published it is the only party the record can
// vouch for at all.
func askedOr(asked, publisher record.Driver) record.Driver {
	if asked != "" {
		return asked
	}
	return publisher
}

// targetOr prefers what the record remembers over what a branch name
// can be read to say.
//
// The recorded value is the planner's own: it was written at mint from
// the slug and the port the planner held apart, so it is the answer and
// not an inference about one. The reading below survives for the
// records that carry no target — one minted before subjects had the
// field, or a branch dockhand did not mint — where a coarse answer
// still beats none.
func targetOr(recorded, branch, port string) string {
	if recorded != "" {
		return recorded
	}
	return targetOf(branch, port)
}

// targetOf reads what a change moves its port to out of the branch
// name. A branch is dockhand/<slug>, and a slug is the port and the
// target joined by a hyphen — "jq-1.8.2", "jq-checksums", "jq-rev1" —
// so the remainder after the port is what the audit wants to record.
//
// It is a reading of a name and not a fact any note carries, and it
// refuses to guess past what it can justify: a slug that does not begin
// with the port keeps its whole self as the target, rather than being
// split at the first hyphen. Splitting blindly would name "1" as the
// target of "jq-1.8.2" the moment the note's port were empty, and a
// wrong value in an audit is worse than a coarse one.
func targetOf(branch, port string) string {
	slug := strings.TrimPrefix(branch, git.BranchNamespace)
	if port == "" {
		return slug
	}
	if rest, ok := strings.CutPrefix(slug, port+"-"); ok {
		return rest
	}
	return slug
}

// stamp is how the audit writes a moment: RFC 3339 in UTC. The rows
// outlive the machine's timezone settings and are compared against each
// other, so they are all written in the one zone that cannot move.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
