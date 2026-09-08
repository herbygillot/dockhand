package cli

import (
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/publish"
)

// The machine publish permission: one build-time constant, spent in one
// place.
//
// Ring 3 is other people's attention. dockhand spends it by pushing the
// fork branch and by creating or editing a pull request against
// upstream, and nothing else it does costs anybody anything they did not
// ask for. The unattended road that spends it — a resident dispatcher's
// publish stage — is built, gated, paced and tested; what the 2026-09-06
// ruling settled is WHICH changes it may spend on, and publish.Grant is
// where that answer is spelled.
//
// IT IS A CONSTANT AND NOT A FLAG so that no invocation, no environment
// variable and no configuration file can be the thing that changed it.
// There is no configuration file in dockhand at all, and this is the
// permission such a file would most want to grant.
//
// THE ZERO VALUE IS THE REFUSAL, all the way down. publish.GrantNothing
// is publish.Grant's zero, so every operation built anywhere else —
// every test, every future composition root, every caller who never
// heard of this file — refuses unattended publication because it never
// granted one. A field named for a permission WITHHELD would have
// inverted that and nobody would have noticed until a pass had opened
// pull requests.
//
// It is GrantSimpleBumps: a machine may open a pull request for a change
// change.Judge called Simple, that has passed verification, and that is
// not held — and the hold is BORN FROM THE CROSSING, so a change that
// takes its port out of stable is held and no unattended act will
// publish it. That crossing is the only hard gate, and it admits 99.945%
// of twelve months of real moves.
const machineGrant = publish.GrantSimpleBumps

// findTree is tree.Find under a name this package can state its reason
// for: the ports tree the working directory is in. It is a one-line
// wrapper because the discovery is best-effort and the caller wants a
// root or nothing, where tree.Find speaks in errors.
func findTree(dir string) (string, error) { return tree.Find(dir) }
