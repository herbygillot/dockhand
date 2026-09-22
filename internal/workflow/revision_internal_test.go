package workflow

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// The scope of a revision prepared onto a contribution is settled before
// the branch moves: an edit's own scope must keep the contribution's
// membership, and a contribution recorded without one, an adopted branch's,
// takes the first scope an edit gives it.
func TestRevisedScopeKeepsTheContributionsMembership(t *testing.T) {
	t.Parallel()
	member := func(name string) record.ReleaseMember {
		return record.ReleaseMember{Target: record.Target{Name: name, Portfile: "devel/fixture/Portfile", Subport: name}}
	}
	one := &record.ReleaseScope{Input: record.ReleaseInput{Portfile: "devel/fixture/Portfile", Before: "1", After: "2"}, Affected: []record.ReleaseMember{member("fixture")}}
	same := &record.ReleaseScope{Input: one.Input, Affected: []record.ReleaseMember{{Target: member("fixture").Target, After: record.ReleaseState{Version: "2"}}}}
	wider := &record.ReleaseScope{Input: one.Input, Affected: []record.ReleaseMember{member("fixture"), member("fixture-sibling")}}
	var e *Engine
	scope, err := e.revisedScope(t.Context(), one, same, record.Source{}, record.Platform{})
	require.NoError(t, err)
	require.Equal(t, same, scope, "the edit's own scope, with the membership kept")
	scope, err = e.revisedScope(t.Context(), nil, wider, record.Source{}, record.Platform{})
	require.NoError(t, err)
	require.Equal(t, wider, scope, "an unscoped contribution takes the edit's scope")
	_, err = e.revisedScope(t.Context(), one, wider, record.Source{}, record.Platform{})
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.ErrorContains(t, err, "would change the contribution's release scope")
	scope, err = e.revisedScope(t.Context(), nil, nil, record.Source{}, record.Platform{})
	require.NoError(t, err)
	require.Nil(t, scope, "nothing to carry and nothing edited")
}

// The message of a revision prepared onto a contribution is the author's,
// with the update's subject laid over the first line under the name the
// message carries, and the update's references added once.
func TestRevisedMessageKeepsTheAuthorsBodyAndTrailers(t *testing.T) {
	t.Parallel()
	original := "py-fixture: update to 2\n\nWhy the update matters.\n\nCloses: https://trac.macports.org/ticket/1"
	closes := record.Reference{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/1"}
	see := record.Reference{Relation: record.ReferenceSee, URL: "https://trac.macports.org/ticket/2"}
	message, err := revisedMessage(original, "py313-fixture", "rebuild", []record.Reference{closes, see})
	require.NoError(t, err)
	require.Equal(t, "py-fixture: rebuild\n\nWhy the update matters.\n\nCloses: https://trac.macports.org/ticket/1\nSee: https://trac.macports.org/ticket/2", message, "the stub's name stays over the carrier's; the cited ticket is not repeated")
	message, err = revisedMessage("hand-made subject\n\nbody", "fixture", "", nil)
	require.NoError(t, err)
	require.Equal(t, "hand-made subject\n\nbody", message, "no subject and no references change nothing")
	message, err = revisedMessage("hand-made subject\n\nbody", "fixture", "update to 3", nil)
	require.NoError(t, err)
	require.Equal(t, "fixture: update to 3\n\nbody", message, "a message without the port's name takes the target's")
	_, err = revisedMessage(original, "fixture", "py-fixture: doubled", nil)
	require.Error(t, err, "a subject carrying the name is refused as the editor refuses it")
}
