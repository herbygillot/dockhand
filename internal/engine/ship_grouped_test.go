package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// groupedPreparer updates jq's Portfile, whose jq-doc subport is versioned
// apart, and reports broken's patch as rejected.
type groupedPreparer struct {
	*fakePreparer
	broken string
}

func (p groupedPreparer) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	const file = "textproc/jq/Portfile"
	before, data, err := p.repo.File(ctx, string(r.Source.Tree), file)
	if err != nil {
		return editprep.Result{}, err
	}
	snapshot := func(text []byte) macports.Snapshot {
		versions := versionLine.FindAllSubmatch(text, -1)
		return macports.Snapshot{Ports: map[string]macports.PortInfo{
			"jq":     {Name: "jq", Version: string(versions[0][1])},
			"jq-doc": {Name: "jq-doc", Version: string(versions[1][1])},
		}}
	}
	was := snapshot(data)
	after := []byte(strings.Replace(string(data), "version "+was.Ports[r.Selection.Selector].Version, "version "+r.Release.Version, 1))
	edit := git.FileEdit{Path: file, Before: before, After: after, Mode: before.Mode}
	tree, err := p.repo.EditTree(ctx, string(r.Source.Tree), []git.FileEdit{edit})
	result := editprep.Result{Target: model.Target{Name: r.Selection.Selector, Portfile: file}, Release: r.Release,
		PreparedTree: model.ObjectID(tree), Files: []git.FileEdit{edit}, Fidelity: []portedit.Fidelity{{Before: was, After: snapshot(after)}}}
	if r.Selection.Selector == p.broken {
		result.Patches = []patchcheck.Result{{Name: "patch-" + p.broken, Checked: true, Detail: "1 out of 1 hunks failed"}}
	}
	return result, err
}

// Each grouped subport's rejected patch holds its own edit and the check:
// the last update's alone were read, held on the first port's edit, so a
// first subport's broken patch queued a check (Codex's workflow review of
// 1caa6c3d, finding 2, its probe).
func TestEachGroupedSubportsPatchHoldsItsOwnEdit(t *testing.T) {
	t.Parallel()
	for _, broken := range []string{"jq", "jq-doc"} {
		t.Run(broken, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			testsupport.WriteFiles(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nrevision 0\nsubport jq-doc {\nversion 1.0.1\n}\n"})
			testsupport.Git(t, f.upstream, "add", "-A")
			testsupport.Git(t, f.upstream, "commit", "-qm", "two independently versioned subports")
			e, p := f.withPreparer(t)
			e.Preparer = groupedPreparer{p, broken}
			usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq"), port("jq-doc")}}})
			e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
			release := func(current, next string) *model.Release {
				return &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: next, CurrentVersion: current}, Version: next, Forge: "github", Tag: "v" + next}
			}
			done := e.ship(t.Context(), shipTarget{Port: "jq", Version: "1.8.1", Release: release("1.7.1", "1.8.1"), Name: "grouped",
				Also: []OutdatedPort{{Port: "jq-doc", Newest: "1.0.2", Release: release("1.0.1", "1.0.2")}}},
				shipOptions{Origin: model.OriginServe, To: ShipCheck, Where: Where{Environments: []model.Environment{{Provider: "command"}}}, Answered: model.AnsweredNobody})
			require.True(t, done.Tidied, done.Problem)
			var heldPort string
			require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
				edits, err := r.Edits(done.Branch.ID)
				for _, edit := range edits {
					if len(edit.Held) > 0 {
						heldPort = edit.Port
					}
				}
				return err
			}))
			require.Equal(t, broken, heldPort, "each subport's preparation hold must stay on its own edit")
			require.Nil(t, done.Run, "a failed patch in any grouped update holds the check")
		})
	}
}
