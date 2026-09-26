package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// Tested on states what the environment reported, as MacPorts' template
// has it, and the runs behind its results. Without a report it names the
// release and the tools the environment stated, and a release dockhand
// doesn't know keeps its Darwin version: never a Darwin version read as
// macOS's.
func TestTestedOnSaysWhatTheEnvironmentWas(t *testing.T) {
	tahoe := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	tart := []model.GuestExecution{{ID: "tart_7y62p4sigena6xlr", ProviderRef: "dockhand-check-run-x-tahoe-1"}}
	for _, test := range []struct {
		name        string
		environment model.Environment
		observed    model.Observed
		runs        []model.GuestExecution
		want        string
	}{
		{"reported, with Xcode", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsXcode},
			model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Xcode: "26.6", XcodeBuild: "17F42", Tools: "26.6.0.0.1781586589"}, tart,
			"macOS 26.6.2 25G71 arm64\nXcode 26.6 17F42 · tart: built in a clean VM, run tart_7y62p4sigena6xlr\n\n"},
		{"reported, with the tools", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsCommandLine},
			model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Tools: "26.6.0.0.1781586589"},
			append(tart, model.GuestExecution{ID: "tart_b3kq9wz0m1xv4ce7"}),
			"macOS 26.6.2 25G71 arm64\nCommand Line Tools 26.6.0.0.1781586589 · tart: built in a clean VM, runs tart_7y62p4sigena6xlr and tart_b3kq9wz0m1xv4ce7\n\n"},
		{"not reported", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsXcode}, model.Observed{}, nil,
			"macOS 26 (Tahoe) arm64\nXcode, its version not recorded · tart: built in a clean VM\n\n"},
		{"an unknown release", model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "30", Architecture: "arm64"}}, model.Observed{}, nil,
			"Darwin 30 arm64\nDeveloper tools not recorded · tart: built in a clean VM\n\n"},
		{"a workflow run", model.Environment{Provider: "github"}, model.Observed{},
			[]model.GuestExecution{{ID: "github_q2w8e4r6t1y3u5i7", ProviderRef: "https://github.com/ada/macports-ports/actions/runs/123"}},
			"Developer tools not recorded · github: MacPorts' CI workflow in the author's fork, run https://github.com/ada/macports-ports/actions/runs/123\n\n"},
	} {
		require.Equal(t, test.want, testedOn(test.environment, test.observed, test.runs), test.name)
	}
}
