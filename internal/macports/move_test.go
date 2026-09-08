package macports

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The transcription's edges, each one a shape base handles in a way the
// obvious lexicographic (epoch, version, revision) order would get wrong.
// Three of these are named in the design because a draft had them
// backwards; the rest are the ordinary cases that would otherwise be
// asserted by nothing.
//
// Read the rows against the Tcl quoted on Move. `upgrades` is the whole
// question — would base install To over an install at From — and the
// three observations beside it are what publish's Direction reads to
// decide whether an epoch is owed.
func TestMoveTranscribesPlanUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name             string
		from, to         Identity
		upgrades         bool
		moved, epochMove bool
		cmp              int
	}{{
		// The plain case: a bump. Nothing consults epoch at all, because
		// vercmp already says the tree is newer and the override is only
		// ever reached from inside a skip.
		name:     "an ordinary version bump upgrades",
		from:     Identity{Epoch: "0", Version: "1.2.3", Revision: "0"},
		to:       Identity{Epoch: "0", Version: "1.2.4", Revision: "0"},
		upgrades: true, moved: true, cmp: 1,
	}, {
		// The design's first named edge. An epoch DECREASE is inert: no
		// branch in _plan_upgrade consults a lower tree epoch, so a
		// version that moved forward upgrades regardless of what the
		// epoch did behind it.
		name:     "an epoch decrease with the version forward still upgrades",
		from:     Identity{Epoch: "2", Version: "1.2.3", Revision: "0"},
		to:       Identity{Epoch: "1", Version: "1.2.4", Revision: "0"},
		upgrades: true, moved: true, epochMove: true, cmp: 1,
	}, {
		// The design's second named edge, and the one that costs a
		// person something: `revision 2` becoming `revision 0` at an
		// unchanged version is the shape a downgrade's revision reset
		// takes, and base skips it. vercmp(2, 0) >= 0 is the skip, and
		// the override cannot fire because the version string did not
		// move.
		name:     "a revision reset at the same version string skips",
		from:     Identity{Epoch: "0", Version: "1.2.3", Revision: "2"},
		to:       Identity{Epoch: "0", Version: "1.2.3", Revision: "0"},
		upgrades: false, cmp: 0,
	}, {
		// The design's third named edge. VerCmp calls versions that
		// differ only in separators equal — its own doc says so — so the
		// skip fires, and only the `ne` string compare in the override
		// can tell these two apart. An epoch bump then rescues it.
		name:     "vercmp-equal but string-different upgrades via the ne override",
		from:     Identity{Epoch: "0", Version: "1.0", Revision: "0"},
		to:       Identity{Epoch: "1", Version: "1-0", Revision: "0"},
		upgrades: true, moved: true, epochMove: true, cmp: 0,
	}, {
		// The rule the whole comparison exists to state: a version that
		// goes backwards without an epoch is a change nobody ever
		// installs. publish.Direction.EpochOwed is Moved && !Upgrades,
		// and this is the row it fires on.
		name:     "a bare downgrade strands every install",
		from:     Identity{Epoch: "0", Version: "1.24.0", Revision: "0"},
		to:       Identity{Epoch: "0", Version: "1.23.1", Revision: "0"},
		upgrades: false, moved: true, cmp: -1,
	}, {
		name:     "a downgrade that moves the epoch with it upgrades",
		from:     Identity{Epoch: "0", Version: "1.24.0", Revision: "0"},
		to:       Identity{Epoch: "1", Version: "1.23.1", Revision: "0"},
		upgrades: true, moved: true, epochMove: true, cmp: -1,
	}, {
		// The `ne` guard from the other side: an epoch bump alone buys
		// nothing, which is why the epoch edit and the version edit are
		// one change and never two.
		name:     "an epoch bump at an unchanged version string does nothing",
		from:     Identity{Epoch: "0", Version: "1.2.3", Revision: "0"},
		to:       Identity{Epoch: "1", Version: "1.2.3", Revision: "0"},
		upgrades: false, epochMove: true, cmp: 0,
	}, {
		name:     "a revision bump at the same version upgrades",
		from:     Identity{Epoch: "0", Version: "1.2.3", Revision: "0"},
		to:       Identity{Epoch: "0", Version: "1.2.3", Revision: "1"},
		upgrades: true, cmp: 0,
	}, {
		name:     "an identity that did not move is not an upgrade",
		from:     Identity{Epoch: "0", Version: "1.2.3", Revision: "1"},
		to:       Identity{Epoch: "0", Version: "1.2.3", Revision: "1"},
		upgrades: false, cmp: 0,
	}, {
		// Revisions go through vercmp too, so 10 is newer than 9 rather
		// than lexicographically older — the reason Move does not reach
		// for strconv on this field.
		name:     "revisions are ordered by VerCmp and not by string",
		from:     Identity{Epoch: "0", Version: "1.2.3", Revision: "9"},
		to:       Identity{Epoch: "0", Version: "1.2.3", Revision: "10"},
		upgrades: true, cmp: 0,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := Move(tc.from, tc.to)
			require.NoError(t, err)
			assert.True(t, m.Compared, "a successful comparison is compared")
			assert.Equal(t, tc.upgrades, m.Upgrades, "Upgrades")
			assert.Equal(t, tc.moved, m.Moved, "Moved")
			assert.Equal(t, tc.epochMove, m.EpochMoved, "EpochMoved")
			// sign is vercmp_test.go's: only VerCmp's sign is part of the
			// ordering, and the magnitude is a byte difference.
			assert.Equal(t, tc.cmp, sign(m.Cmp), "Cmp")
			assert.Equal(t, tc.from, m.From)
			assert.Equal(t, tc.to, m.To)
		})
	}
}

// Rule 7 at the boundary: a Movement that could not be computed says so
// in its own zero, so a caller that ignores the error cannot read the
// refusal as "it does not upgrade".
func TestMoveRefusesAnIdentityItCannotRead(t *testing.T) {
	ok := Identity{Epoch: "0", Version: "1.2.3", Revision: "0"}
	for _, tc := range []struct {
		name     string
		from, to Identity
		want     error
	}{
		{"no from version", Identity{Epoch: "0", Revision: "0"}, ok, ErrNoVersion},
		{"no to version", ok, Identity{Epoch: "0", Revision: "0"}, ErrNoVersion},
		{"unset from epoch", Identity{Version: "1.2.3", Revision: "0"}, ok, ErrEpochNotAnInteger},
		{"unset to epoch", ok, Identity{Version: "1.2.4", Revision: "0"}, ErrEpochNotAnInteger},
		{"non-integer epoch", ok, Identity{Epoch: "1a", Version: "1.2.4", Revision: "0"}, ErrEpochNotAnInteger},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := Move(tc.from, tc.to)
			require.ErrorIs(t, err, tc.want)
			assert.False(t, m.Compared, "a refusal is not a comparison")
			assert.False(t, m.Upgrades, "a refusal never claims an upgrade")
		})
	}
}
