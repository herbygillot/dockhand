package cli

import (
	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBuildProviderDefaultsAndExplicitChoices(t *testing.T) {
	for _, test := range []struct {
		name, command   string
		args            []string
		config          app.Config
		provider, tests string
	}{
		{name: "keep failed chooses local", command: "bump", args: []string{"--keep-failed"}, provider: "tart", tests: "declared"},
		{name: "bump automatic", command: "bump", provider: "auto"},
		{name: "revision automatic", command: "bump-revision", provider: "auto"},
		{name: "verify stays local", command: "verify", provider: "tart", tests: "declared"},
		{name: "explicit local", command: "bump", args: []string{"--provider=tart"}, provider: "tart", tests: "declared"},
		{name: "explicit remote", command: "bump", args: []string{"--provider=github"}, provider: "github", tests: "workflow"},
		{name: "configured remote", command: "bump", config: app.Config{VerificationProvider: "github"}, provider: "github", tests: "workflow"},
		{name: "configured image", command: "bump", config: app.Config{Tart: tart.Config{Image: "custom"}}, provider: "tart", tests: "declared"},
		{name: "explicit image", command: "bump", args: []string{"--image=custom"}, provider: "tart", tests: "declared"},
		{name: "explicit tests", command: "bump", args: []string{"--tests=skip"}, provider: "tart", tests: "skip"},
		{name: "workflow policy", command: "bump", args: []string{"--tests=workflow"}, provider: "github", tests: "workflow"},
		{name: "capacity", command: "bump", args: []string{"--capacity=1"}, provider: "tart", tests: "declared"},
		{name: "variants", command: "bump", args: []string{"--variant=+debug"}, provider: "tart", tests: "declared"},
		{name: "source policy", command: "bump", args: []string{"--from-source"}, provider: "tart", tests: "declared"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: test.command}
			cmd.Flags().StringArray("variant", nil, "")
			var build buildOptions
			build.flags(cmd, test.config)
			require.NoError(t, cmd.ParseFlags(test.args))
			config, err := build.config(cmd, test.config)
			require.NoError(t, err)
			require.Equal(t, test.provider, config.VerificationProvider)
			require.Equal(t, test.tests, build.tests)
		})
	}
}
