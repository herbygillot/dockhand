package tart

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// An environment with Xcode builds in the release's Xcode image, and one
// with the Command Line Tools in its base image (decision 23, amended).
func TestTheEnvironmentsToolsChooseTheImage(t *testing.T) {
	t.Parallel()
	for tools, want := range map[model.DeveloperTools]string{
		model.DeveloperToolsXcode:       "dockhand-xcode-tahoe",
		model.DeveloperToolsCommandLine: "dockhand-base-tahoe",
		"":                              "dockhand-base-tahoe",
	} {
		mac := newMac(guestResults{State: "finished"})
		mac.images = append(mac.images, "dockhand-xcode-tahoe")
		job := tartJob(t, 1)
		job.Environment.DeveloperTools = tools
		require.NoError(t, testProvider(mac).Execute(t.Context(), job, &fakeBuild{}))
		vm := "dockhand-check-run-7-tahoe-1"
		require.Equal(t, []string{"clone " + want + " " + vm, "start " + vm, "reach " + vm + " as " + want, "delete " + vm}, mac.events, tools)
	}
}

// The remedy for a release without Xcode is the command that makes its
// Xcode image.
func TestTheRemedyForXcodeIsItsImage(t *testing.T) {
	t.Parallel()
	unmet := model.Unmet{Target: "libharbor", Environment: model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsCommandLine}, Needs: model.RequiresXcode}
	require.Equal(t, "dockhand setup tart tahoe --xcode <Xcode .xip, or a folder of them> makes macOS 26's Xcode image", testProvider(newMac()).Remedy(unmet))

	// One whose minimum_xcodeversions the image's Xcode doesn't meet is
	// given that Xcode (the sand-runner port).
	unmet.Environment.DeveloperTools, unmet.Needs = model.DeveloperToolsXcode, model.RequiresXcodeVersion("27.0")
	require.Equal(t, `tahoe = "27.0" under [providers.tart.xcode], then dockhand setup tart tahoe --xcode <Xcode 27.0 .xip>, gives macOS 26's Xcode image Xcode 27.0`, testProvider(newMac()).Remedy(unmet))
}
