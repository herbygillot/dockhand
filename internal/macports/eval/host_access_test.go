package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestModeledObservationTracksHostFilesAtAccessTime(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"open", "relative-stat", "source", "captured-open", "captured-source", "symlink"} {
		t.Run(operation, func(t *testing.T) {
			e := liveEvaluator(t)
			tree := fixtureTree(t)
			external := filepath.Join(t.TempDir(), "host-data")
			require.NoError(t, os.WriteFile(external, []byte("set host_version 1.2.3"), 0600))
			captured := filepath.Join(tree.Root(), "devel/host/files/data")
			putFile(t, tree.Root(), "devel/host/files/data", "set host_version 1.2.3")
			link := filepath.Join(tree.Root(), "devel/host/files/link")
			require.NoError(t, os.Symlink(external, link))
			paths := map[string]string{"open": external, "captured-open": captured, "symlink": link}
			body := fmt.Sprintf("set handle [open {%s} r]\nset value [read $handle]\nclose $handle\n", paths[operation])
			switch operation {
			case "relative-stat":
				body = "set exists [file exists ../../../../../../tmp/dockhand-host-state]\n"
			case "source":
				body = fmt.Sprintf("source {%s}\n", external)
			case "captured-source":
				body = fmt.Sprintf("source {%s}\n", captured)
			}
			putFile(t, tree.Root(), "devel/host/Portfile", "PortSystem 1.0\nname host\nversion 1\n"+body)
			targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "host"})
			require.NoError(t, err)
			bound, err := tree.Select(targets[0])
			require.NoError(t, err)
			got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
			require.NoError(t, err)
			require.Equal(t, operation != "captured-open" && operation != "captured-source", got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
		})
	}
}
