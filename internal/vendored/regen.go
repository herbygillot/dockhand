package vendored

import (
	"context"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/tool"
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
	// Tools resolves the generators a family runs and the archiver it
	// reads a distfile with — the run's one finder, handed down.
	Tools *tool.Finder
}

// Regenerator is one vendored-block family's whole story: whether a
// context carries its block, which distfiles that block supplies (so
// the checksum machinery leaves them alone), any reason a change beside
// the block would be dishonest, and the regeneration itself. The
// intents iterate the registry in vendored/families instead of growing
// an if-chain — the contract exists so the next family (Zig, when its
// resolver lands) is a registration, not another lobe on a planner.
//
// There are two refusals rather than one because the two intents ask
// different questions of the same block. A bump regenerates it, so what
// it needs to know is whether regenerating would state something untrue.
// A refresh has no version move to hang a regeneration on: it changes
// the port's own checksums and leaves the block exactly where it is, so
// what it needs to know is whether the block can stay put while they
// move. A family can answer yes to one and no to the other, and cargo
// does.
type Regenerator interface {
	// Kind names the block.
	Kind() Kind
	// Present reports whether this evaluated context carries the block.
	Present(vals info.Values) bool
	// Veto returns a reason regeneration must refuse, judged before
	// any network is spent; ok reports a veto. The zero Regenerator
	// behaviour is no veto.
	Veto(vals info.Values) (reason string, ok bool)
	// VetoRefresh returns a reason a checksum refresh beside this block
	// must refuse — the port's own distfiles re-hashed while the block
	// keeps the values it has; ok reports a veto.
	VetoRefresh(vals info.Values) (reason string, ok bool)
	// Supplied names the distfiles the block contributes — the set the
	// checksum machinery must not treat as the port's own. It runs
	// before any fetch, so rc.Fetched is empty here.
	Supplied(ctx context.Context, rc Regen) ([]string, error)
	// Regenerate produces the block edits for the target version —
	// usually one, but a family may maintain sibling blocks (cargo's
	// registry and github forms travel together).
	Regenerate(ctx context.Context, rc Regen) ([]edit.Edit, error)
}
