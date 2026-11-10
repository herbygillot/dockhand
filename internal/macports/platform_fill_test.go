package macports

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macos"
)

// A row borrows what it doesn't tell by the version each fact belongs to,
// from any source: Xcode's SDKs from a row with the same Xcode, other tools
// alike; the tools' SDKs and clang from a row with the same tools, another
// Xcode alike (#87, #17).
func TestARowBorrowsFactsByTheirVersion(t *testing.T) {
	t.Parallel()
	table := macos.FactsTable{Facts: []macos.Facts{
		{Darwin: 24, Architecture: "arm64", Profile: macos.ProfileXcode, Xcode: "16.4", Tools: "16.4", XcodeSDKs: []string{"MacOSX.sdk", "MacOSX15.5.sdk -> MacOSX.sdk"}, Source: macos.Source{Kind: macos.SourceTart}},
		{Darwin: 25, Architecture: "x86_64", Profile: macos.ProfileXcode, Xcode: "26.6", Tools: "26.0", Clang: "1700.3.19.1", SDKs: []string{"MacOSX26.sdk"}, Source: macos.Source{Kind: macos.SourceRunner}},
	}}
	intel := macos.Facts{Darwin: 24, Architecture: "x86_64", Profile: macos.ProfileXcode, Xcode: "16.4", Tools: "26.0", Source: macos.Source{Kind: macos.SourceBuildbot}}
	fill(table, &intel)
	require.Equal(t, []string{"MacOSX.sdk", "MacOSX15.5.sdk -> MacOSX.sdk"}, intel.XcodeSDKs, "by its Xcode, its tools another's")
	require.Equal(t, "1700.3.19.1", intel.Clang, "by its tools, its Xcode another's, from a runner's row")
	require.Equal(t, []string{"MacOSX26.sdk"}, intel.SDKs)
}
