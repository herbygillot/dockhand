package portindex_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
)

// The index says how its ports write a maintainer: each entry naming them,
// as written, with how many Portfiles write it so, most first. A
// Portfile's subports count once, an entry naming someone else counts
// for nothing, and a field that can't be read is passed over.
func TestTheIndexSaysHowItsPortsWriteAMaintainer(t *testing.T) {
	t.Parallel()
	index, err := portindex.Open(writeIndex(t, []indexRecord{
		{"alpha", "name alpha portdir devel/alpha maintainers {{gmail.com:herby.gillot @herbygillot} openmaintainer}"},
		{"alpha-doc", "name alpha-doc portdir devel/alpha maintainers {{gmail.com:herby.gillot @herbygillot} openmaintainer}"},
		{"beta", "name beta portdir net/beta maintainers {{gmail.com:herby.gillot @herbygillot}}"},
		{"gamma", "name gamma portdir net/gamma maintainers {@HerbyGillot {@ada example.org:ada}}"},
		{"delta", "name delta portdir sysutils/delta maintainers {{@ada example.org:ada}}"},
		{"broken", `name broken portdir sysutils/broken maintainers \{`},
		{"epsilon", "name epsilon portdir sysutils/epsilon"},
	}, nil))
	require.NoError(t, err)
	spellings, err := index.Spellings("@herbygillot")
	require.NoError(t, err)
	require.Equal(t, []portindex.MaintainerSpelling{
		{Maintainer: macports.Maintainer{"gmail.com:herby.gillot", "@herbygillot"}, Portfiles: 2},
		{Maintainer: macports.Maintainer{"@HerbyGillot"}, Portfiles: 1},
	}, spellings)

	spellings, err = index.Spellings("ada@example.org")
	require.NoError(t, err)
	require.Equal(t, []portindex.MaintainerSpelling{{Maintainer: macports.Maintainer{"@ada", "example.org:ada"}, Portfiles: 2}}, spellings)

	spellings, err = index.Spellings("@nobody")
	require.NoError(t, err)
	require.Empty(t, spellings)
}
