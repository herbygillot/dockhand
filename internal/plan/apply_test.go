package plan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/info"
)

// scripted answers Apply's two questions from a list, in order, so that
// the whole of Apply — the precondition, the write, the re-evaluation,
// the equality against the prediction, the restore — runs with no Tcl
// shell behind it. That it can is the point of the Snapshotter seam:
// Apply's only demand on an evaluator is a snapshot before and a
// snapshot after, and an interface saying exactly that is what keeps
// the shell out of the dependency set of everything that prints a plan.
type scripted struct {
	answers []answer
	calls   int
	portdir string
}

// answer is one scripted reply: a snapshot, or the refusal an evaluator
// gives when the file in front of it will not evaluate.
type answer struct {
	snap info.Snapshot
	err  error
}

func (s *scripted) Snapshot(_ context.Context, portdir string, _ info.VariantSet) (info.Snapshot, error) {
	s.portdir = portdir
	s.calls++
	if s.calls > len(s.answers) {
		return nil, fmt.Errorf("scripted oracle asked %d times, scripted for %d", s.calls, len(s.answers))
	}
	a := s.answers[s.calls-1]
	return a.snap, a.err
}

// at builds the one-context snapshot the fixture below evaluates to.
func at(version string) info.Snapshot {
	return info.Snapshot{
		info.SubportKey{Subport: "jq"}: info.Values{Name: "jq", Version: version},
	}
}

// portdirWith writes a Portfile into a temp portdir and returns the
// directory and the bytes, so a test can hash the precondition off the
// same source Apply will read.
func portdirWith(t *testing.T, src string) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, macports.PortfileName)
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	return dir, []byte(src)
}

const jqPortfile = "name jq\nversion 1.8.1\nrevision 0\n"

var versionEdit = []edit.Edit{
	{Start: 16, End: 21, Old: "1.8.1", New: "1.8.2", Reason: "version"},
}

func TestApplyWritesWhenTheObservedDeltaIsThePredictedOne(t *testing.T) {
	dir, src := portdirWith(t, jqPortfile)
	p := &Plan{
		Portdir:        dir,
		PortfileSHA256: edit.FileSHA256(src),
		Edits:          versionEdit,
		Predicted:      FromDelta(at("1.8.1").Diff(at("1.8.2"))),
	}
	ev := &scripted{answers: []answer{{snap: at("1.8.1")}, {snap: at("1.8.2")}}}

	observed, err := p.Apply(context.Background(), ev)
	require.NoError(t, err)
	assert.Equal(t, 2, ev.calls, "one snapshot before the write and one after")
	assert.Equal(t, dir, ev.portdir)
	assert.Equal(t, p.Predicted, FromDelta(observed))

	after, rerr := os.ReadFile(filepath.Join(dir, macports.PortfileName))
	require.NoError(t, rerr)
	assert.Equal(t, "name jq\nversion 1.8.2\nrevision 0\n", string(after))
}

// A plan either does precisely what it said or does nothing. The
// scripted oracle is what makes the second half testable: a real
// evaluator would have to be talked into disagreeing with a correct
// prediction, and here disagreement is just the next answer.
func TestApplyRestoresWhenTheObservedDeltaIsNotThePredictedOne(t *testing.T) {
	dir, src := portdirWith(t, jqPortfile)
	p := &Plan{
		Portdir:        dir,
		PortfileSHA256: edit.FileSHA256(src),
		Edits:          versionEdit,
		Predicted:      FromDelta(at("1.8.1").Diff(at("1.8.2"))),
	}
	// The world says the edit landed somewhere else entirely.
	ev := &scripted{answers: []answer{{snap: at("1.8.1")}, {snap: at("9.9.9")}}}

	_, err := p.Apply(context.Background(), ev)
	require.ErrorIs(t, err, ErrMismatch)

	after, rerr := os.ReadFile(filepath.Join(dir, macports.PortfileName))
	require.NoError(t, rerr)
	assert.Equal(t, jqPortfile, string(after), "the pre-apply bytes are back")
}

// The edited Portfile failing to evaluate is the same promise reached by
// a different route, and it restores too.
func TestApplyRestoresWhenTheEditedPortfileWillNotEvaluate(t *testing.T) {
	dir, src := portdirWith(t, jqPortfile)
	p := &Plan{
		Portdir:        dir,
		PortfileSHA256: edit.FileSHA256(src),
		Edits:          versionEdit,
		Predicted:      FromDelta(at("1.8.1").Diff(at("1.8.2"))),
	}
	boom := errors.New("Tcl said no")
	ev := &scripted{answers: []answer{{snap: at("1.8.1")}, {err: boom}}}

	_, err := p.Apply(context.Background(), ev)
	require.ErrorIs(t, err, ErrMismatch)
	require.ErrorIs(t, err, boom)

	after, rerr := os.ReadFile(filepath.Join(dir, macports.PortfileName))
	require.NoError(t, rerr)
	assert.Equal(t, jqPortfile, string(after))
}

// A Portfile that moved under the plan is refused before anything is
// evaluated: drift is cheaper to detect than to survive.
func TestApplyRefusesADriftedPortfileWithoutEvaluating(t *testing.T) {
	dir, _ := portdirWith(t, jqPortfile)
	p := &Plan{
		Portdir:        dir,
		PortfileSHA256: edit.FileSHA256([]byte("name jq\nversion 1.8.0\nrevision 0\n")),
		Edits:          versionEdit,
	}
	ev := &scripted{answers: []answer{{snap: at("1.8.1")}}}

	_, err := p.Apply(context.Background(), ev)
	require.ErrorIs(t, err, ErrDrift)
	assert.Contains(t, err.Error(), dir, "the message names what drifted")
	assert.Zero(t, ev.calls, "nothing is evaluated for a plan that cannot apply")

	after, rerr := os.ReadFile(filepath.Join(dir, macports.PortfileName))
	require.NoError(t, rerr)
	assert.Equal(t, jqPortfile, string(after))
}
