package command

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/version"
)

// Streams are the standard streams a command reads and writes.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

const rebuilding = `dockhand is being rebuilt as v3 (docs/design-v3.md). The v3 commands
arrive with the roadmap's step 5. Until then, the working tool is v2,
tagged v2-final:

  git worktree add ../dockhand-v2 v2-final
  make -C ../dockhand-v2 build BINARY="$HOME/.local/bin/dockhand-v2"`

// Run executes the command line in args.
func Run(ctx context.Context, args []string, streams Streams) error {
	root := &cobra.Command{
		Use:           "dockhand",
		Short:         "Author, check, and submit changes to MacPorts ports",
		Long:          "Author, check, and submit changes to MacPorts ports.\n\n" + rebuilding,
		Version:       version.Current().String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("dockhand {{.Version}}\n")
	root.SetArgs(args)
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	return root.ExecuteContext(ctx)
}
