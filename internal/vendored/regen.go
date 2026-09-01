package vendored

import (
	"context"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/tempdir"
)

// Regen is everything the planner knows at regeneration time, bundled
// so each family takes what it needs: cargo reads the fetched
// distfiles, go asks the shadow, the next family will want something
// else again.
type Regen struct {
	// Src and CST are the Portfile being planned against.
	Src []byte
	CST *syntax.Script
	// Handle and Vals are the context and its evaluated state at the
	// current version.
	Handle port.Handle
	Vals   info.Values
	// Shadow is a handle over the edited Portfile at the target
	// version, with its evaluated state — the portgroup's own answers
	// about where the port is going.
	Shadow     port.Handle
	ShadowVals info.Values
	// Fetched are the local paths of the target version's own
	// distfiles, already downloaded and checksummed.
	Fetched []string
	// Fetch downloads what a family needs beyond those — the git-crate
	// tarballs, checksummed as they land.
	Fetch distfile.Fetcher
	// TempDir stages whatever a generator needs on disk.
	TempDir tempdir.Root
}

// Regenerator is one vendored-block family's whole story: whether a
// context carries its block, which distfiles that block supplies (so
// the checksum machinery leaves them alone), any reason regeneration
// would be dishonest, and the regeneration itself. Bump iterates a
// registry of these instead of growing an if-chain — the contract
// exists so the third family (Zig, when its resolver lands) is a
// registration, not another lobe on the planner.
type Regenerator interface {
	// Kind names the block.
	Kind() Kind
	// Present reports whether this evaluated context carries the block.
	Present(vals info.Values) bool
	// Veto returns a reason regeneration must refuse, judged before
	// any network is spent; ok reports a veto. The zero Regenerator
	// behaviour is no veto.
	Veto(vals info.Values) (reason string, ok bool)
	// Supplied names the distfiles the block contributes — the set the
	// checksum machinery must not treat as the port's own. It runs
	// before any fetch, so rc.Fetched is empty here.
	Supplied(ctx context.Context, rc Regen) ([]string, error)
	// Regenerate produces the block edits for the target version —
	// usually one, but a family may maintain sibling blocks (cargo's
	// registry and github forms travel together).
	Regenerate(ctx context.Context, rc Regen) ([]edit.Edit, error)
}
