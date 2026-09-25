package command

import (
	"bufio"
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/version"
)

// Streams are the standard streams a command reads and writes.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	// interactive stands in for a terminal in tests.
	interactive bool
	// lines reads answers from In; one reader for the whole command, so
	// input it buffered for one question is there for the next.
	lines *bufio.Reader
}

// terminal reports whether the input is an interactive terminal, the only
// place a command may ask a question. /dev/null is a character device too,
// so this asks the terminal driver rather than the file's mode.
func (s Streams) terminal() bool {
	if s.interactive {
		return true
	}
	file, ok := s.In.(*os.File)
	return ok && isTerminal(file.Fd())
}

const rebuilding = `dockhand is being rebuilt as v3 (docs/design-v3.md). The commands below
are the first of it; check, status, and serve arrive with the rest of the
roadmap's step 5. Until then, the working tool is v2, tagged v2-final:

  git worktree add ../dockhand-v2 v2-final
  make -C ../dockhand-v2 build BINARY="$HOME/.local/bin/dockhand-v2"`

// Run executes the command line in args.
func Run(ctx context.Context, args []string, streams Streams) error {
	var settings settings
	if streams.lines == nil && streams.In != nil {
		streams.lines = bufio.NewReader(streams.In)
	}
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
	settings.flags(root)
	root.AddGroup(&cobra.Group{ID: "work", Title: "Start or enter work:"})
	for _, command := range []*cobra.Command{
		initCommand(&settings, streams),
		startCommand(&settings, streams),
		adoptCommand(&settings, streams),
		pathCommand(&settings, streams),
	} {
		command.GroupID = "work"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "author", Title: "Author:"})
	for _, command := range []*cobra.Command{
		updateCommand(&settings, streams),
		checksumsCommand(&settings, streams),
	} {
		command.GroupID = "author"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "review", Title: "Prepare for review:"})
	for _, command := range []*cobra.Command{
		tidyCommand(&settings, streams),
		submitCommand(&settings, streams),
	} {
		command.GroupID = "review"
		root.AddCommand(command)
	}
	root.AddGroup(&cobra.Group{ID: "occasional", Title: "Occasional:"})
	for _, command := range []*cobra.Command{
		restoreCommand(&settings, streams),
	} {
		command.GroupID = "occasional"
		root.AddCommand(command)
	}
	root.SetArgs(args)
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	return root.ExecuteContext(ctx)
}
