package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// Tested on states what the environment reported, as MacPorts' template
// has it. Without a report it names the release and the tools the
// environment stated, and a release dockhand doesn't know keeps its Darwin
// version: never a Darwin version read as macOS's.
func TestTestedOnSaysWhatTheEnvironmentWas(t *testing.T) {
	tahoe := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	for _, test := range []struct {
		name        string
		environment model.Environment
		observed    model.Observed
		want        string
	}{
		{"reported, with Xcode", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsXcode},
			model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Xcode: "26.6", XcodeBuild: "17F42", Tools: "26.6.0.0.1781586589"},
			"macOS 26.6.2 25G71 arm64\nXcode 26.6 17F42 · tart: built in a clean VM\n\n"},
		{"reported, with the tools", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsCommandLine},
			model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Tools: "26.6.0.0.1781586589"},
			"macOS 26.6.2 25G71 arm64\nCommand Line Tools 26.6.0.0.1781586589 · tart: built in a clean VM\n\n"},
		{"not reported", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsXcode}, model.Observed{},
			"macOS 26 (Tahoe) arm64\nXcode, its version not recorded · tart: built in a clean VM\n\n"},
		{"an unknown release", model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "30", Architecture: "arm64"}}, model.Observed{},
			"Darwin 30 arm64\nDeveloper tools not recorded · tart: built in a clean VM\n\n"},
		{"no platform", model.Environment{Provider: "github"}, model.Observed{},
			"Developer tools not recorded · github: MacPorts' CI workflow in the author's fork\n\n"},
	} {
		require.Equal(t, test.want, testedOn(test.environment, test.observed), test.name)
	}
}
