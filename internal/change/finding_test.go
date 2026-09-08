package change

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/statestore"

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/intent/bump"
	"github.com/herbygillot/dockhand/internal/record"
)

// EVERY KIND A PLANNER WRITES MUST BE A KIND THE RECORD ADMITS, and for
// the whole of the overhaul two of them were not.
//
// A plan-time finding's Kind is a plain string; stamp converts it
// through findingKind's exhaustive table and returns ErrUnknownFinding
// for anything else; MintIn returns that error, so the Amend writes
// nothing at all — no branch, no record. record.FindingKind spelled
// "instruction" while every planner wrote "instruction-comment", and did
// not spell "patches-unchecked" at all. So `dockhand bump` failed
// outright on any port carrying a maintainer instruction comment, and on
// any port whose patches could not be checked against a fetched source
// — 49 and 69 Portfiles respectively on the real macports-ports tree.
//
// THE RULE WAS ENFORCED IN ONE DIRECTION ONLY. record.FindingKind's doc
// says "a presenter that invents a kind writes a note a later build
// cannot classify", which is right, and findingKind enforces exactly
// that. Nothing checked the other direction — that every word a planner
// writes is a word the table admits. This is that check.
//
// It imports the planners, which this package never does outside a test:
// intent does not import change, so there is no cycle, and the census
// belongs beside the table it guards rather than in whichever package
// happens to sit above both.
func TestEveryKindAPlannerWritesIsOneTheRecordAdmits(t *testing.T) {
	for _, word := range []string{
		intent.FindingInstruction,
		bump.FindingPatchesUnchecked,
		bump.FindingPatchUnrelocated,
	} {
		kind, ok := findingKind(word)
		assert.True(t, ok, "a planner writes %q and the record refuses it, which fails the whole mint", word)
		assert.Equal(t, word, string(kind))
	}
}

// AND THE RECORD'S OWN SPELLINGS ARE THE PLANNERS'. Pinned separately so
// that moving either constant fails here with the two words in front of
// the reader, rather than only failing the loop above.
func TestTheRecordsKindsAreSpelledAsThePlannersWriteThem(t *testing.T) {
	assert.Equal(t, "instruction-comment", string(record.KindInstruction))
	assert.Equal(t, "patches-unchecked", string(record.KindPatchesUnchecked))
	assert.Equal(t, "patch-unrelocated", string(record.KindPatchUnrelocated))
}

// AN INSTRUCTION COMMENT MUST NOT BLOCK A PERSON — ruled 8 September
// 2026 — and it did not merely block one: it failed the mint for every
// invoker, because its kind was refused three lines before any gate got
// a say.
//
// The ruling's behaviour is already built and was simply unreachable.
// The finding carries Proposed, publish.Authorize's fourth gate refuses
// an open proposal for record.Machine and lets a person through with an
// advisory, and `dockhand dismiss` is the person's answer. All this
// verb had to do was let the word through.
func TestAMintCarriesAnInstructionCommentInsteadOfRefusingIt(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	base := primary(t, repo)
	sha, content, err := Commit(ctx, repo, prepared(t, repo), base)
	require.NoError(t, err)

	var c record.Change
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		c, err = MintIn(tx, Minting{
			ID: "chg-01", Branch: "dockhand/jq-1.8", Tip: sha, Content: content, Slug: "jq-1.8",
			Subjects:    []record.Subject{{Port: "jq", Names: []string{"jq"}, Portdir: "sysutils/jq", Target: "1.8"}},
			Destination: record.ToBranch,
			Base:        record.Base{Sha: base},
			Prov:        Provenance{AskedBy: record.Human, Via: record.MintedSingle},
			Findings: []plan.Finding{{
				Kind: intent.FindingInstruction, Ports: []string{"jq"},
				Source: "sysutils/jq/Portfile", Quote: "revbump dependents when this moves",
				Disposition: plan.Proposed,
			}},
		}, now)
		return err
	}), "the mint must not fail on a comment somebody wrote in a Portfile")

	require.Len(t, c.Findings, 1)
	assert.Equal(t, record.KindInstruction, c.Findings[0].Kind)
	assert.Equal(t, record.Proposed, c.Findings[0].Disposition,
		"Proposed is what refuses the machine and advises the person; the ruling is this value")
	assert.Equal(t, "revbump dependents when this moves", c.Findings[0].Quote,
		"the comment is quoted verbatim, because dockhand weighs nothing here")
}

// AND A PATCH IT COULD NOT CHECK IS A STATEMENT, NOT A QUESTION. It
// carries Accepted deliberately — nothing here is for a person to
// answer, so it must not hold an unattended publication the way a
// proposal does (bump.patchesUnchecked). That distinction was moot while
// the kind was refused: both failed the mint identically.
func TestAMintCarriesAnUncheckedPatchFindingWithoutHoldingAnything(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	base := primary(t, repo)
	sha, content, err := Commit(ctx, repo, prepared(t, repo), base)
	require.NoError(t, err)

	var c record.Change
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		c, err = MintIn(tx, Minting{
			ID: "chg-02", Branch: "dockhand/jq-1.9", Tip: sha, Content: content, Slug: "jq-1.9",
			Subjects:    []record.Subject{{Port: "jq", Names: []string{"jq"}, Portdir: "sysutils/jq", Target: "1.9"}},
			Destination: record.ToBranch,
			Base:        record.Base{Sha: base},
			Prov:        Provenance{AskedBy: record.Human, Via: record.MintedSingle},
			Findings: []plan.Finding{{
				Kind: bump.FindingPatchesUnchecked, Ports: []string{"jq"},
				Criterion:   "patch check unavailable: jq's 2 patchfiles were not checked against the new source because no distfile was fetched",
				Disposition: plan.Accepted,
			}},
		}, now)
		return err
	}))

	require.Len(t, c.Findings, 1)
	assert.Equal(t, record.KindPatchesUnchecked, c.Findings[0].Kind)
	assert.Equal(t, record.Accepted, c.Findings[0].Disposition,
		"a statement, not a question: it must not hold an unattended publication")
}
