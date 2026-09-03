package upstream

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/upstream/forge"
)

// notAForge names the carrier styles that deliberately have no forge
// coordinates, with the reason each has none. A style reaching this
// list is a decision; a style reaching neither this list nor a spec is
// a port whose upstream dockhand silently never asks about — it falls
// back to the livecheck alone and reports a verdict that reads as if
// two witnesses had spoken.
//
// Modelled on portstyle's own setupSkiplist, and for the same reason: a
// table that is only ever added to is a table that ages, and the aging
// is invisible until a port is judged wrong.
var notAForge = map[portstyle.Type]string{
	portstyle.None:               "the zero value: no style located, so no coordinates to read",
	portstyle.VersionLine:        "a bare version line carries no source coordinates at all",
	portstyle.RevisionLine:       "a revision line carries neither a version nor a source",
	portstyle.SetVariable:        "a Tcl variable holding the version; whatever fetches is spelled elsewhere",
	portstyle.Perl5Setup:         "a distribution archive, not a repository",
	portstyle.RubySetup:          "a distribution archive, not a repository",
	portstyle.PureSetup:          "a distribution archive, not a repository",
	portstyle.AspellDictSetup:    "a distribution archive, not a repository",
	portstyle.HunspellDictSetup:  "a distribution archive, not a repository",
	portstyle.ElpaSetup:          "a distribution archive, not a repository",
	portstyle.LuarocksSetup:      "a distribution archive, not a repository",
	portstyle.X11FontSetup:       "a distribution archive, not a repository",
	portstyle.CrossBinutilsSetup: "a toolchain release, not a repository",
	portstyle.CrossGccSetup:      "a toolchain release, not a repository",
	portstyle.CrossGdbSetup:      "a toolchain release, not a repository",
	portstyle.GoToolchainSetup:   "a toolchain release, not a repository",
	portstyle.ZigToolchainSetup:  "a toolchain release, not a repository",
}

// styles walks portstyle.Type by its iota until String stops
// recognizing a member. The set is closed and contiguous and String is
// held exhaustive by the linter, so the walk sees every member — the
// one added tomorrow included, which is the only reason to walk it
// rather than list it here.
func styles(t *testing.T) []portstyle.Type {
	t.Helper()
	var out []portstyle.Type
	for s := portstyle.Type(0); s.String() != "unknown style"; s++ {
		out = append(out, s)
		require.Less(t, len(out), 256, "portstyle.Type walk did not terminate; String has a case for a value beyond the enum")
	}
	require.NotEmpty(t, out)
	return out
}

// The mirror: every carrier style is either forge-resolvable through a
// spec of its own, forge-resolvable through the family it delegates to,
// or listed above with a reason it is neither.
func TestEveryCarrierStyleIsResolvedOrExcused(t *testing.T) {
	for _, s := range styles(t) {
		_, spec := coordSpecs[s]
		_, delegated := delegatedFamilies[s]
		reason, excused := notAForge[s]

		named := 0
		for _, b := range []bool{spec, delegated, excused} {
			if b {
				named++
			}
		}
		switch named {
		case 1:
			if excused {
				assert.NotEmpty(t, reason, "%s is excused without a reason", s)
			}
		case 0:
			t.Errorf("%s has no coordinate spec, no delegation and no reason to lack both", s)
		default:
			t.Errorf("%s is claimed more than once: spec=%v delegated=%v excused=%v", s, spec, delegated, excused)
		}
	}
}

// A delegation names the family whose options the coordinates land in,
// so a namespace with no spec behind it is a style that resolves to
// nothing at run time and says nothing at compile time.
func TestDelegationsNameRealFamilies(t *testing.T) {
	for style, families := range delegatedFamilies {
		require.NotEmpty(t, families, "%s delegates to nothing", style)
		for _, ns := range families {
			_, ok := specsByNS[ns]
			assert.True(t, ok, "%s delegates to %q, which is no family's namespace", style, ns)
		}
	}
}

// go.setup keeps its own coordinates and borrows the tag scheme from
// whichever family its domain selects, so every namespace it borrows
// from has to be one.
func TestGoTagNamespacesAreRealFamilies(t *testing.T) {
	for _, ns := range goTagNamespaces {
		_, ok := specsByNS[ns]
		assert.True(t, ok, "go.setup borrows a tag scheme from %q, which is no family's namespace", ns)
	}
}

// The other mirror, against forge's own registry: a forge nothing
// carries is a forge no port can be resolved on, and a carrier pointing
// at a forge the registry does not list is a URL builder nobody
// reviewed. forge.None is deliberately outside All — it is what a
// repository is on when it is on none of them — and it is reached
// through go.setup's domain rather than by a spec naming it.
func TestEveryForgeHasACarrierAndEveryCarrierAKnownForge(t *testing.T) {
	carried := map[*forge.Forge]bool{}
	for style, spec := range coordSpecs {
		if spec.f == nil {
			assert.Equal(t, portstyle.GoSetup, style,
				"%s names no forge; only go.setup resolves one from its domain", style)
			continue
		}
		carried[spec.f] = true
		assert.Contains(t, forge.All, spec.f, "%s carries %s, which forge.All does not list", style, spec.f.Name)
	}
	for _, f := range forge.All {
		assert.True(t, carried[f], "%s is a known forge no carrier style resolves to", f.Name)
	}
	assert.NotContains(t, forge.All, forge.None, "the absence of a forge is not a forge")
}

// A spec's namespace is what its option names are built from, so two
// specs sharing one would silently overwrite each other in specsByNS —
// and the delegation lookups read that index.
func TestNamespacesAreUniqueAndIndexed(t *testing.T) {
	assert.Len(t, specsByNS, len(coordSpecs), "two carrier styles share a namespace; specsByNS lost one")
	for style, spec := range coordSpecs {
		require.NotEmpty(t, spec.ns, "%s has no namespace", style)
		assert.Equal(t, spec, specsByNS[spec.ns], "%s is not what its namespace indexes to", style)
	}
}
