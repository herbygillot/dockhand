package intent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
)

// inertEdit is a leading comment: bytes a Portfile evaluates past, so
// the prediction it produces is empty and the witness rule is the only
// thing standing between it and a plan.
func inertEdit() []edit.Edit {
	return []edit.Edit{{Start: 0, End: 0, Old: "", New: "# nothing to see\n", Reason: "inert"}}
}

// The refused zero, stated as its own fact rather than inferred from
// the empty-delta test: NoWitness is what an intent that declared
// nothing has, and every other kind is something a planner asserted.
func TestTheUndeclaredWitnessIsTheZeroValue(t *testing.T) {
	var unset WitnessKind
	assert.Equal(t, NoWitness, unset)
	assert.Empty(t, unset.Excuses(), "an undeclared witness excuses nothing")
	assert.Equal(t, "no witness", unset.String())
}

// Every kind in the catalogue says which fields it excuses, and the
// answer comes from the kind rather than from the call site. This is
// the whole difference from the sentence it replaced: the claim is
// owned by the package that checks it.
func TestEachWitnessNamesTheFieldsItExcuses(t *testing.T) {
	assert.Equal(t, []info.Field{info.FieldChecksums}, WitnessFetched.Excuses())
	assert.Equal(t, []info.Field{info.FieldVersion}, WitnessVersionWritten.Excuses())
	assert.Equal(t, []info.Field{info.FieldRevision}, WitnessRevisionWritten.Excuses())
	assert.Empty(t, WitnessRidersInert.Excuses(),
		"housekeeping claims nothing moved; it does not excuse a field's silence")

	for _, k := range []WitnessKind{
		WitnessFetched, WitnessVersionWritten, WitnessRevisionWritten, WitnessRidersInert,
	} {
		assert.NotEqual(t, "unknown witness", k.String(), "kind %d has no words", int(k))
	}
}

// A witness may only excuse fields its intent declared it may change.
// The two are written in the same FinishOpts by the same planner, so a
// disagreement is that planner's bug — and it is caught on a run that
// moved something, not only on the empty ones, because those are the
// runs a suite actually has.
func TestFinishRefusesAWitnessForAFieldTheIntentMayNotChange(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 0")
	src, opts := subject(t, h)
	id := Identity{Intent: "bump-revision", Slug: "finishee-rev1", Summary: "finishee: revbump"}

	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}
	// A revbump that claims a fetch it never made: the checksums are not
	// a field this change was ever about.
	opts.Witness = WitnessFetched
	edits := []edit.Edit{revisionEdit(t, src, "0", "1")}

	_, err := Finish(context.Background(), h, src, edits, id, opts)
	require.ErrorIs(t, err, ErrWitnessOverreaches)
	assert.Contains(t, err.Error(), "checksums", "the refusal names the field it could not account for")

	// The coherent declaration plans, on the same edits and the same run.
	opts.Witness = WitnessRevisionWritten
	p, err := Finish(context.Background(), h, src, edits, id, opts)
	require.NoError(t, err)
	assert.NotEmpty(t, p.Predicted)
}

// An empty prediction needs a witness that accounts for it. A kind that
// excuses no field accounts for an emptiness only where the intent
// declared no field it may change — housekeeping's claim, exactly —
// and offering it beside a MayChange set is changing the subject.
func TestAnEmptyPredictionNeedsAWitnessThatExcusesSomething(t *testing.T) {
	h := synthetic(t, "version 1.0\nrevision 0")
	src, opts := subject(t, h)
	id := Identity{Intent: "bump", Slug: "finishee-1.0", Summary: "finishee: update to 1.0"}

	// The intent says it may move the revision, moves nothing, and offers
	// the housekeeping witness for the silence.
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}
	opts.Witness = WitnessRidersInert
	_, err := Finish(context.Background(), h, src, inertEdit(), id, opts)
	require.ErrorIs(t, err, ErrWitnessExcusesNothing)
	// The three refusals stay distinguishable — nothing declared,
	// something that overreaches, something that explains nothing — so a
	// caller never has to read the words to tell them apart.
	require.NotErrorIs(t, err, ErrNoWitness)
	require.NotErrorIs(t, err, ErrWitnessOverreaches)

	// Housekeeping's own shape: nothing may change, and nothing is
	// excused, because the claim is that nothing moves.
	opts.MayChange = nil
	p, err := Finish(context.Background(), h, src, inertEdit(), id, opts)
	require.NoError(t, err)
	assert.Empty(t, p.Predicted)

	// And the same emptiness with a witness that DOES name a field it
	// may change is a plan.
	opts.MayChange = map[info.Field]bool{info.FieldRevision: true}
	opts.Witness = WitnessRevisionWritten
	p, err = Finish(context.Background(), h, src, inertEdit(), id, opts)
	require.NoError(t, err)
	assert.Empty(t, p.Predicted)
}
