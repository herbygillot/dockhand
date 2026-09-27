package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A plan reads each release in its own context with MacPorts itself: this
// Mac's natively, any other modelled in the same session, its tools from
// the facts table (decision 7). A port that asks for Xcode before macOS 13
// needs it on Monterey and not on Sequoia, whichever release the Mac runs.
// An environment that states its tools is read with them, this Mac's
// release too: a port known to fail without Xcode is excluded with the
// Command Line Tools and built with Xcode.
func TestAPlanReadsEachReleaseInItsOwnContext(t *testing.T) {
	f := setup(t)
	f.options.Tclsh = testsupport.MacPortsTclsh(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "xdemo", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"devel/xdemo/Portfile": `PortSystem 1.0
name xdemo
version 1
categories devel
license MIT
maintainers nomaintainer
homepage https://example.invalid
description demo
long_description demo
if {${os.major} < 22} {
    use_xcode yes
}
`, "devel/xtools/Portfile": `PortSystem 1.0
name xtools
version 1
categories devel
license MIT
maintainers nomaintainer
homepage https://example.invalid
description demo
long_description demo
if {${xcodeversion} eq "none"} {
    known_fail yes
}
`})
	run(t, branch.Worktree, "add", "-A")
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	monterey := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}}
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{monterey, sequoia}})
	require.NoError(t, err)
	require.Empty(t, plan.Unresolved)
	_, ok := plan.Target("xdemo")
	require.True(t, ok)
	require.True(t, plan.NeedsXcodeIn(monterey, "xdemo"))
	require.False(t, plan.NeedsXcodeIn(sequoia, "xdemo"))

	for _, version := range []string{"25", "24"} {
		tools := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: version, Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsCommandLine}
		xcode := tools
		xcode.DeveloperTools = model.DeveloperToolsXcode
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tools}})
		require.NoError(t, err)
		require.Empty(t, plan.Unresolved)
		_, planned := plan.Target("xtools")
		require.False(t, planned, "Darwin %s with the Command Line Tools has no Xcode, so xtools is known to fail", version)
		plan, err = e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{xcode}})
		require.NoError(t, err)
		_, planned = plan.Target("xtools")
		require.True(t, planned, "Darwin %s with Xcode builds xtools", version)
	}
}
