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
	require.Equal(t, "dockhand providers setup tart tahoe --xcode <Xcode .xip, or a folder of them> makes macOS 26's Xcode image", testProvider(newMac()).Remedy(unmet))
}
