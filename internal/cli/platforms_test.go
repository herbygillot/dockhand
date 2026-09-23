package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// --os names Tart images, one per release, for a verification without
// dependents; what cannot build that way is refused before anything is
// opened or recorded.
func TestOSRefusesWhatCannotBuildOnNamedReleases(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"verify", "jq", "--os", "sonoma", "--provider", "github"}, want: "runner matrix"},
		{args: []string{"verify", "jq", "--os", "sonoma", "--image", "custom"}, want: "--image names one image"},
		{args: []string{"verify", "jq", "--os", "sonoma", "--dependents"}, want: "--dependents"},
		{args: []string{"bump", "jq", "--os", "sonoma"}, want: "unknown flag"},
	} {
		config := app.Config{Repository: "/missing/repository", DBPath: filepath.Join(t.TempDir(), "absent", "state.db")}
		var out bytes.Buffer
		err := Run(t.Context(), test.args, Streams{Out: &out, Err: &out}, config)
		require.ErrorContains(t, err, test.want, "%v", test.args)
		require.NoFileExists(t, config.DBPath)
	}
	configured := app.Config{Repository: "/missing/repository", DBPath: filepath.Join(t.TempDir(), "absent", "state.db"), VerificationProvider: "github"}
	var out bytes.Buffer
	err := Run(t.Context(), []string{"verify", "jq", "--os", "available"}, Streams{Out: &out, Err: &out}, configured)
	require.ErrorContains(t, err, "runner matrix", "a configured GitHub provider is refused as a named one is")
}

// Under auto, naming a release is a choice of Tart, like any Tart option.
func TestOSChoosesTart(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{Use: "verify"}
	cmd.Flags().StringArray("os", nil, "")
	var build buildOptions
	build.flags(cmd, app.Config{})
	require.NoError(t, cmd.ParseFlags([]string{"--os=sonoma"}))
	config, err := build.config(cmd, app.Config{})
	require.NoError(t, err)
	require.Equal(t, "tart", config.VerificationProvider)
	require.True(t, verificationSettingsChanged(cmd), "a named release is a change of settings, not the recorded build")
}
