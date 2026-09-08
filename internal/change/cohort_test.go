package change

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
)

// proposalOf is a change carrying one Proposed cohort finding, which is
// the only thing Cohort reads.
func proposalOf(cands ...record.Candidate) record.Change {
	return record.Change{ID: "chg-01", State: record.ChangeMinted, Findings: []record.Finding{{
		Kind: record.KindABIDependents, Disposition: record.Proposed,
		Criterion: "install name libjq.1.dylib -> libjq.2.dylib", Candidates: cands,
	}}}
}

func TestCohortWithoutAmendmentsIsTheProposalItself(t *testing.T) {
	c := proposalOf(
		record.Candidate{Port: "mise", Proposed: true, Reason: "depends_lib on jq"},
		record.Candidate{Port: "obsolete", Reason: "replaced by another port"},
	)
	got, err := Cohort(c, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, c.Findings[0].Candidates, got,
		"the ports examined and left out travel beside the ones put forward; a decision no reader can see is one nobody can disagree with")
}

func TestCohortRefusesAChangeWithNoProposal(t *testing.T) {
	_, err := Cohort(record.Change{ID: "chg-01"}, nil, nil)
	require.ErrorIs(t, err, ErrNoProposal)

	answered := proposalOf(record.Candidate{Port: "mise", Proposed: true})
	answered.Findings[0].Disposition = record.Accepted
	_, err = Cohort(answered, nil, nil)
	assert.ErrorIs(t, err, ErrNoProposal, "an answer is given once")
}

func TestCohortExcludesByFoldedName(t *testing.T) {
	c := proposalOf(
		record.Candidate{Port: "ImageMagick", Proposed: true, Reason: "depends_lib on jq"},
		record.Candidate{Port: "mise", Proposed: true, Reason: "depends_lib on jq"},
	)
	got, err := Cohort(c, []string{"imagemagick"}, nil)
	require.NoError(t, err)
	assert.False(t, got[0].Proposed)
	assert.Equal(t, excludedReason, got[0].Reason)
	assert.Equal(t, "ImageMagick", got[0].Port, "the candidate's own spelling is what is recorded")
	assert.True(t, got[1].Proposed)
	assert.True(t, c.Findings[0].Candidates[0].Proposed, "the amendment is pure: the record it read is untouched")
}

func TestCohortRefusesANameTheProposalDoesNotPutForward(t *testing.T) {
	c := proposalOf(record.Candidate{Port: "mise", Proposed: true})
	_, err := Cohort(c, []string{"nothing-like-it"}, nil)
	require.ErrorIs(t, err, ErrUnknownMember)
	assert.Contains(t, err.Error(), "mise", "the refusal says what could have been named instead")
}

func TestCohortRefusesAnEmptyCohort(t *testing.T) {
	c := proposalOf(record.Candidate{Port: "mise", Proposed: true})
	_, err := Cohort(c, []string{"mise"}, nil)
	assert.ErrorIs(t, err, ErrEmptyCohort, "a cohort with no members is not a smaller cohort; it is no commit at all")
}

func TestCohortSeatsAWithheldMemberWithItsSiblingNamed(t *testing.T) {
	c := proposalOf(
		record.Candidate{Port: "gegl", Proposed: true, Reason: "depends_lib on jq"},
		record.Candidate{Port: "gegl-devel", Proposed: true, Solo: true, Over: "gegl",
			Reason: "depends_lib on jq — bumped here, and not built"},
	)
	got, err := Cohort(c, nil, []string{"GEGL-devel"})
	require.NoError(t, err)
	assert.True(t, got[1].Forced)
	assert.True(t, got[1].Solo, "it is still the withheld member; what changed is that a person seated it")
	assert.Equal(t, "depends_lib on jq — forced into the build at the maintainer's request, with gegl deactivated first",
		got[1].Reason, "the base of the sentence is kept, because it is still true")
}

func TestCohortRefusesForcingWhatWasNotWithheld(t *testing.T) {
	c := proposalOf(
		record.Candidate{Port: "mise", Proposed: true, Reason: "depends_lib on jq"},
		record.Candidate{Port: "obsolete", Reason: "replaced"},
	)
	_, err := Cohort(c, nil, []string{"mise"})
	require.ErrorIs(t, err, ErrNotWithheld)
	_, err = Cohort(c, nil, []string{"obsolete"})
	require.ErrorIs(t, err, ErrNotWithheld)
	_, err = Cohort(c, nil, []string{"never-heard-of-it"})
	assert.ErrorIs(t, err, ErrUnknownMember)
}

func TestCohortRefusesASeatItCannotMakeRoomFor(t *testing.T) {
	t.Run("the record does not say what to deactivate", func(t *testing.T) {
		c := proposalOf(record.Candidate{Port: "gegl-devel", Proposed: true, Solo: true, Reason: "withheld"})
		_, err := Cohort(c, nil, []string{"gegl-devel"})
		assert.ErrorIs(t, err, ErrCannotForce)
	})
	t.Run("--exclude took the sibling out of the change", func(t *testing.T) {
		c := proposalOf(
			record.Candidate{Port: "gegl", Proposed: true, Reason: "depends_lib on jq"},
			record.Candidate{Port: "gegl-devel", Proposed: true, Solo: true, Over: "gegl", Reason: "withheld"},
		)
		_, err := Cohort(c, []string{"gegl"}, []string{"gegl-devel"})
		assert.ErrorIs(t, err, ErrCannotForce, "there would be nothing to deactivate; the person picks one flag")
	})
	t.Run("two forced members deactivate each other", func(t *testing.T) {
		c := proposalOf(
			record.Candidate{Port: "a", Proposed: true, Solo: true, Over: "b", Reason: "withheld"},
			record.Candidate{Port: "b", Proposed: true, Solo: true, Over: "a", Reason: "withheld"},
			record.Candidate{Port: "c", Proposed: true, Reason: "depends_lib on jq"},
		)
		_, err := Cohort(c, nil, []string{"a", "b"})
		assert.ErrorIs(t, err, ErrForcedConflict, "deactivating one sibling seats one of them, and nothing seats both")
	})
}
