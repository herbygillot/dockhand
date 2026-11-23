package fidelity

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// A source moved at the version a port is at may change its fetch fields
// and checksums, and nothing else: its revision, its version and every
// other port stay (#121, #195).
func TestASourceMoveAtTheVersionKeepsTheRevision(t *testing.T) {
	t.Parallel()
	port := func(revision int, sites, checksums string) macports.PortInfo {
		return macports.PortInfo{Name: "fixture", Version: "1.2.4", Revision: revision, Options: map[string]string{"master_sites": sites, "checksums": checksums}}
	}
	sibling := macports.PortInfo{Name: "sibling", Version: "1.0", Options: map[string]string{"master_sites": "https://example.com"}}
	before := macports.Snapshot{Ports: map[string]macports.PortInfo{"fixture": port(2, "tarball", "sha256 0"), "sibling": sibling}}
	after := macports.Snapshot{Ports: map[string]macports.PortInfo{"fixture": port(2, "releases", "sha256 1"), "sibling": sibling}}
	require.Empty(t, SourceMove(before, after, "fixture", "sha256 1").UnexpectedChanges)
	after.Ports["fixture"] = port(0, "releases", "sha256 1")
	require.NotEmpty(t, SourceMove(before, after, "fixture", "sha256 1").UnexpectedChanges, "the revision dropped")
	after.Ports["fixture"] = port(2, "releases", "sha256 1")
	require.Contains(t, SourceMove(before, after, "fixture", "sha256 2").UnexpectedChanges, "fixture.checksums differ from intended values")
	moved := sibling
	moved.Options = map[string]string{"master_sites": "https://example.org"}
	after.Ports["sibling"] = moved
	require.NotEmpty(t, SourceMove(before, after, "fixture", "sha256 1").UnexpectedChanges, "another port moved")
}
