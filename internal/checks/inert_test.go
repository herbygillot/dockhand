package checks

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/reuse"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// recordHooks are hooks that give each revision's change records from a
// table, by revision; the rest do nothing.
type recordHooks struct {
	records map[model.RevisionID]map[string]model.ChangeRecord
	asked   []model.RevisionID
}

func (h *recordHooks) BeforeEnvironment(context.Context, model.Run, model.Plan, model.Environment, model.Source) []string {
	return nil
}
func (h *recordHooks) AfterBuild(context.Context, model.Run, model.Revision) error { return nil }
func (h *recordHooks) AfterFinish(context.Context, model.Run)                      {}
func (h *recordHooks) Changes(_ context.Context, revision model.Revision) (map[string]model.ChangeRecord, error) {
	h.asked = append(h.asked, revision.ID)
	if found, ok := h.records[revision.ID]; ok {
		return found, nil
	}
	return nil, errors.New("no records")
}

// omnidiffDirectory commits omnidiff's directory with a Portfile of
// maintainers, and reads its tree.
func omnidiffDirectory(t *testing.T, root, maintainers string) model.ObjectID {
	t.Helper()
	testsupport.WriteFiles(t, root, map[string]string{
		"textproc/omnidiff/Portfile":      "PortSystem 1.0\nname omnidiff\nversion 1.2.0\nmaintainers " + maintainers + "\nlicense MIT\n",
		"textproc/omnidiff/files/a.patch": "a\n",
	})
	testsupport.Git(t, root, "add", "-A")
	testsupport.Git(t, root, "commit", "-q", "--allow-empty", "-m", "edit")
	return model.ObjectID(strings.TrimSpace(testsupport.Git(t, root, "rev-parse", "HEAD:textproc/omnidiff")))
}

// An inert difference is the Portfile's lines and both revisions' change
// records together: an openmaintainer line, the records moving only the
// maintainers and the checksum of every option's value, carries over; a
// record that names another field, or a revision without one, carries
// nothing over; and none is judged where nothing gives records.
func TestTheDriverConfirmsAnInertChangeByBothRevisionsRecords(t *testing.T) {
	t.Parallel()
	c := service(t)
	root := t.TempDir()
	testsupport.Git(t, root, "init", "-q")
	checkout, err := git.Open(t.Context(), root, git.Program{Env: os.Environ()})
	require.NoError(t, err)
	c.Checkout = checkout
	origin := c.queued(t, "omnidiff", model.Environment{Provider: "local", Prefix: "/opt/local"})
	then := omnidiffDirectory(t, root, "{@herbygillot gmail.com:herby.gillot}")
	now := omnidiffDirectory(t, root, "{@herbygillot gmail.com:herby.gillot} openmaintainer")
	license := omnidiffDirectory(t, root, "{@herbygillot gmail.com:herby.gillot}\nlicense BSD")

	var built model.Revision
	require.NoError(t, c.Store.View(t.Context(), c.Repository, func(r store.Reader) error {
		built, err = r.Revision(origin.Revision)
		return err
	}))
	current := model.Revision{ID: "rev_now"}
	record := func(fields ...model.FieldChange) map[string]model.ChangeRecord {
		return map[string]model.ChangeRecord{"textproc/omnidiff": {Directory: "textproc/omnidiff", Policy: 9,
			Ports: []model.SubportChange{{Port: "omnidiff", Kind: model.SubportChanged, Fields: append([]model.FieldChange{{Field: "version", From: "1.1.0", To: "1.2.0"}}, fields...)}}}}
	}
	hooks := &recordHooks{records: map[model.RevisionID]map[string]model.ChangeRecord{
		built.ID:   record(),
		current.ID: record(model.FieldChange{Field: "maintainers", From: "a", To: "a openmaintainer"}, model.FieldChange{Field: "code options", From: "1", To: "2"}),
	}}
	c.Hooks = hooks
	d := &driver{c: c}

	inert := d.inert(t.Context(), current)
	options, ok := inert(origin.ID, "textproc/omnidiff", then, "textproc/omnidiff", now)
	require.True(t, ok)
	require.Equal(t, []string{"maintainers"}, options)
	_, ok = inert(origin.ID, "textproc/omnidiff", then, "textproc/omnidiff", now)
	require.True(t, ok, "asked again")
	require.Len(t, hooks.asked, 2, "each revision's records read once")

	_, ok = inert(origin.ID, "textproc/omnidiff", then, "textproc/omnidiff", license)
	require.False(t, ok, "a license line isn't inert, whatever the records say")

	hooks.records[current.ID] = record(model.FieldChange{Field: "dependencies", From: "", To: "lib:zlib"})
	_, ok = d.inert(t.Context(), current)(origin.ID, "textproc/omnidiff", then, "textproc/omnidiff", now)
	require.False(t, ok, "a record names another field")

	delete(hooks.records, current.ID)
	_, ok = d.inert(t.Context(), current)(origin.ID, "textproc/omnidiff", then, "textproc/omnidiff", now)
	require.False(t, ok, "no record of the revision now")

	d.c.Hooks = nil
	require.Nil(t, d.inert(t.Context(), current), "nothing gives records")
}

// linting is a port reader whose lint says what it's given.
type linting struct {
	said  macports.Lint
	err   error
	asked []string
}

func (l *linting) Ports(context.Context, model.Source, string, model.Environment, map[string]bool) ([]macports.PortInfo, error) {
	return nil, nil
}
func (l *linting) Directory(context.Context, model.Source, string) (string, error) { return "", nil }
func (l *linting) Lint(_ context.Context, _ model.Source, directory, port string, nitpick bool) (macports.Lint, error) {
	words := directory + " " + port
	if nitpick {
		words += " --nitpick"
	}
	l.asked = append(l.asked, words)
	return l.said, l.err
}

// A result carried over a change to its own Portfile takes what lint says
// of that Portfile now, run on this Mac, with --nitpick for a port the
// branch adds; keeps the earlier Portfile's lines, said so, where lint
// can't run here; and isn't carried where lint finds an error. One carried
// over a dependency's change alone isn't linted.
func TestACarriedResultTakesLintsLinesFromTheHost(t *testing.T) {
	t.Parallel()
	c := service(t)
	origin := c.queued(t, "omnidiff", model.Environment{Provider: "local", Prefix: "/opt/local"})
	reader := &linting{said: macports.Lint{Warnings: []string{"Using full email address for no/open maintainer"}}}
	c.Ports = func() (PortReader, DependencyGraphReader, error) { return reader, nil, nil }
	d := &driver{c: c}
	target := reuse.Target{PlanTarget: model.PlanTarget{ID: "omnidiff", Target: model.Target{Name: "omnidiff"}, Directory: "textproc/omnidiff", Added: true}}
	choose := func(ports ...model.InertPort) reuse.Choice {
		earlier := model.TargetResult{Target: "omnidiff", Outcome: model.OutcomePassed, Lint: []string{"an earlier line"}}
		return reuse.Choice{Reused: map[model.TargetID]reuse.Candidate{"omnidiff": {Result: earlier, Origin: model.GuestExecution{Run: origin.ID}}},
			Carried: map[model.TargetID][]model.InertPort{"omnidiff": ports}}
	}
	own := model.InertPort{Port: "omnidiff", Options: []string{"maintainers"}}

	choice := choose(own)
	require.Empty(t, d.carry(t.Context(), model.Revision{}, []reuse.Target{target}, &choice, map[model.TargetID]lintAnswer{}))
	result := choice.Reused["omnidiff"].Result
	require.Equal(t, []string{"Using full email address for no/open maintainer"}, result.Lint, "lint's lines replaced")
	require.True(t, result.Nitpicked)
	require.Equal(t, &model.Carried{Check: origin.Name(), Commit: "1111111111111111111111111111111111111111", Ports: []model.InertPort{own}, Linted: true}, result.Carried)
	require.Equal(t, []string{"textproc/omnidiff omnidiff --nitpick"}, reader.asked)

	reader.err = errors.New("the evaluator refused it")
	choice = choose(own)
	require.Empty(t, d.carry(t.Context(), model.Revision{}, []reuse.Target{target}, &choice, map[model.TargetID]lintAnswer{}))
	result = choice.Reused["omnidiff"].Result
	require.Equal(t, []string{"an earlier line"}, result.Lint, "the earlier Portfile's lines")
	require.Equal(t, "lint couldn't run on this Mac: the evaluator refused it", result.Carried.LintKept)
	require.False(t, result.Carried.Linted)

	reader.err, reader.said = nil, macports.Lint{Errors: []string{"Unknown license: BSD-ish"}}
	choice = choose(own)
	require.Equal(t, map[model.TargetID][]string{"omnidiff": {"Unknown license: BSD-ish"}}, d.carry(t.Context(), model.Revision{}, []reuse.Target{target}, &choice, map[model.TargetID]lintAnswer{}))

	reader.asked = nil
	choice = choose(model.InertPort{Port: "omnidiff-data", Options: []string{"description"}})
	require.Empty(t, d.carry(t.Context(), model.Revision{}, []reuse.Target{target}, &choice, map[model.TargetID]lintAnswer{}))
	require.Empty(t, reader.asked, "its own Portfile didn't change")
	require.Equal(t, []string{"an earlier line"}, choice.Reused["omnidiff"].Result.Lint)
	require.False(t, choice.Reused["omnidiff"].Result.Carried.Linted)
}
