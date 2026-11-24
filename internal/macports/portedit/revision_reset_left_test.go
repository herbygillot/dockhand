package portedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestRevisionResetLeavesAContextWhoseModeledEvaluationCompleted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "devel/fixture/Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	const portfileText = `PortSystem 1.0
name fixture
categories devel
version 1.2.3
if {${os.major} < 15} {
    catch {exec /usr/bin/true}
    revision 5
} else {
    revision 2
}
master_sites https://example.invalid/${version}
checksums sha256 aaaa size 2
`
	require.NoError(t, os.WriteFile(path, []byte(portfileText), 0600))
	request := Request{Action: model.EditUpdate, Source: model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, Workspace: adopt(t, root), Selection: macports.Selection{Selector: "fixture"}, Version: "1.2.4"}
	s := &Service{Ports: &eval.Evaluator{Executable: testsupport.MacPortsTclsh(t), Adapter: testsupport.BaseAdapter()}}
	input, err := s.load(t.Context(), &request)
	require.NoError(t, err)
	defer input.Close()

	contents, err := portfile.SetVersionLiteral(input.data, input.info.Version, "1.2.4")
	require.NoError(t, err)
	reset, err := s.resetRevision(t.Context(), request, input, contents)
	require.NoError(t, err)

	// The modelled older context evaluated successfully, but its host read
	// leaves it; its revision stays at 5. The native context can reset 2.
	require.Contains(t, string(reset), "revision 5")
	require.Contains(t, string(reset), "revision 0")
}
