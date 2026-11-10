package checks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A failure's detail gains what its log most likely says made it fail,
// marked as read from the log, beside what the provider said, read from
// where the step that failed began (D10, from the beekeeper-studio run's
// finding 2).
func TestAFailuresCauseIsReadFromItsLog(t *testing.T) {
	t.Parallel()
	log := filepath.Join(t.TempDir(), "harbor-cli.log")
	require.NoError(t, os.WriteFile(log, []byte("--->  Building harbor-cli\nsrc/cli.c:3:1: error: expected ';' after expression\nmake: *** [all] Error 1\n"), 0o644))
	require.Equal(t, "from its log: src/cli.c:3:1: error: expected ';' after expression", withCause("", model.TargetResult{Log: log}), "with nothing else said")
	require.Equal(t, "said", withCause("said", model.TargetResult{Log: filepath.Join(t.TempDir(), "gone.log")}), "a log that can't be read says nothing")

	// Read from where the step that failed began: an error a dependency's
	// build printed before it, and went on from, isn't its cause.
	stepped := filepath.Join(t.TempDir(), "hugo.log")
	require.NoError(t, os.WriteFile(stepped, []byte("dep/probe.c:1:1: error: tried and went on\n--->  Building hugo\ncmd/main.c:9:2: error: hugo's own\n"), 0o644))
	steps := []model.LogStep{{Name: model.StepDependencies, Line: 1}, {Name: model.StepInstall, Line: 2}}
	require.Equal(t, "from its log: cmd/main.c:9:2: error: hugo's own", withCause("", model.TargetResult{Log: stepped, Steps: steps}))
	require.Equal(t, "from its log: dep/probe.c:1:1: error: tried and went on", withCause("", model.TargetResult{Log: stepped}), "without steps, from the top")
}

// A result whose tests failed names them from its log, from its test step
// on, whether the target failed or passed under declared tests (the exiv2
// field notes, 2026-10-19).
func TestAResultWhoseTestsFailedNamesThem(t *testing.T) {
	t.Parallel()
	log := filepath.Join(t.TempDir(), "target-1.log")
	require.NoError(t, os.WriteFile(log, []byte("FAIL: a dependency's own\n:info:test The following tests FAILED:\nThe following tests FAILED:\n\t  3 - bugfixes-test (Failed)\nmake: *** [test] Error 8\n"), 0o644))
	steps := []model.LogStep{{Name: model.StepInstall, Line: 1}, {Name: model.StepTest, Line: 2}}
	require.Equal(t, "tests: make test failed · failed tests, from its log: bugfixes-test", withFailedTests("tests: make test failed", model.TargetResult{Log: log, Steps: steps}))
	require.Equal(t, "said", withFailedTests("said", model.TargetResult{Log: filepath.Join(t.TempDir(), "gone.log")}))
}

// A port can have two kept archives offered, its newest and the one
// before: the count says both where they differ, as gnome-voice's "up to
// 221 dependencies" were 223 archives given (check-484).
func TestTheOfferSaysItsArchivesWhereAPortHasTwo(t *testing.T) {
	t.Parallel()
	require.Equal(t, "the guest may install up to 2 dependencies from archives earlier checks kept", offeredWords(2, 2))
	require.Equal(t, "the guest may install up to 221 dependencies from archives earlier checks kept (223 archives, up to two recent archives per dependency)", offeredWords(221, 223))
	require.Equal(t, "the guest may install up to 1 dependency from archives earlier checks kept", offeredWords(1, 1))
}

// A log directory names the release as everything else does, macOS 26 as
// macos26, not by its Darwin version, which isn't macOS's.
func TestLogDirectoriesNameTheMacOSRelease(t *testing.T) {
	t.Parallel()
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	require.Equal(t, "tart-macos26-arm64", environmentSlug(tahoe))
	require.Equal(t, "tart-macos15-x86_64", environmentSlug(model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "x86_64"}}))
	require.Equal(t, "tart-darwin30-arm64", environmentSlug(model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "30", Architecture: "arm64"}}))
	require.Equal(t, "tart-darwin30-arm64", environmentSlug(model.Environment{Provider: "tart", Platform: model.Platform{Version: "30", Architecture: "arm64"}}), "no OS is Darwin's")
	require.Equal(t, "command", environmentSlug(model.Environment{Provider: "command"}))
}
