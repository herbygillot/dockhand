package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/platform"
)

// TestFrameEnumeration asks whether evaluating a port across platform
// frames RECOVERS the distfiles and checksums that a single evaluation
// cannot see. It ships nothing.
//
// It is the precondition for re-deriving the blocks the checksum
// refusals flag. Until the frame carried architecture, this could not
// have been asked: every arch frame returned the same answer, so the
// enumeration would have reported one distinct set and concluded there
// was nothing to recover.
//
//	DOCKHAND_ENUM_TREE=/path/to/ports DOCKHAND_ENUM_PORTS=<file> \
//	  go test ./internal/cli/ -run TestFrameEnumeration -timeout 60m -v
func TestFrameEnumeration(t *testing.T) {
	root, list := os.Getenv("DOCKHAND_ENUM_TREE"), os.Getenv("DOCKHAND_ENUM_PORTS")
	if root == "" || list == "" {
		t.Skip("set DOCKHAND_ENUM_TREE and DOCKHAND_ENUM_PORTS to run")
	}
	b, err := os.ReadFile(list)
	require.NoError(t, err)
	names := strings.Fields(string(b))

	ctx := context.Background()
	pfx, err := prefix.Find(testFinder())
	require.NoError(t, err)
	tr, err := tree.Open(root)
	require.NoError(t, err)

	// The frame space: every release dockhand can name, on both
	// architectures. One pool per frame, every port evaluated in it, so
	// mportinit is paid once per frame rather than once per pair.
	type key struct {
		frame string
		port  string
	}
	seen := map[key]string{}
	var frames []string
	for _, r := range platform.Releases {
		for _, arch := range []string{"arm", "i386"} {
			f := fmt.Sprintf("%s/%s", r.Name, arch)
			frames = append(frames, f)
			p, perr := pool.New(ctx, pfx, 1, eval.WithPlatform(
				info.Platform{OS: "macosx", Major: r.Darwin, Arch: arch}))
			if perr != nil {
				continue
			}
			for _, name := range names {
				target, terr := tr.Resolve(name)
				if terr != nil {
					continue
				}
				h := port.New(tree.Target{Portdir: target.Portdir, Subport: target.Subport}, p.Evaluators()[0])
				vals, verr := h.Values(ctx)
				if verr != nil {
					continue
				}
				seen[key{f, name}] = strings.Join(vals.Distfiles, " ") + " || " + strings.Join(vals.Checksums, " ")
			}
			p.Close()
		}
	}

	for _, name := range names {
		distinct := map[string][]string{}
		for _, f := range frames {
			if v, ok := seen[key{f, name}]; ok && v != " || " {
				distinct[v] = append(distinct[v], f)
			}
		}
		fmt.Fprintf(os.Stderr, "\n%s: %d distinct (distfiles, checksums) set(s) across %d frames\n",
			name, len(distinct), len(frames))
		var keys []string
		for k := range distinct {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			fs := distinct[k]
			fmt.Fprintf(os.Stderr, "  [%d] %s\n       …%s\n", i+1, brief(fs), tail(k))
		}
	}
}

func brief(frames []string) string {
	if len(frames) <= 3 {
		return strings.Join(frames, ", ")
	}
	return fmt.Sprintf("%s … %s (%d frames)", frames[0], frames[len(frames)-1], len(frames))
}

func tail(s string) string {
	if len(s) > 150 {
		return s[:150]
	}
	return s
}
