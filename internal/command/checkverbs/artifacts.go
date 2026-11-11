package checkverbs

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

// ArtifactsCommand prints the archives a branch's latest check built, a
// line a port and platform, for otool and smoke tests by hand (field
// testing's outdated-ports session).
func ArtifactsCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "artifacts [branch | check]",
		Short: "Show the local paths of a check's built archives",
		Long: `Lists archives from the branch's latest check, with one line per passed
port and platform: the port, platform, archive name, and path on this Mac.
Use these paths to inspect or manually test the built files. The output
explains when a port did not pass or its archive was not kept or was removed
by cleanup.

Name a check, such as check-42, to list its archives. With no argument in a
branch's worktree, lists that branch's latest check.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A check is named as logs names one: artifacts check-781 read
			// "no tracked branch named check-781" (field testing, #68).
			check := len(args) == 1 && engine.IsCheckName(args[0])
			if len(args) == 1 && !check {
				if where.Explicit() {
					return fmt.Errorf("name a branch once, as the argument or with -b, -p, or --pr")
				}
				where.Branch = args[0]
			}
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			var run model.Run
			switch {
			case check && where.Explicit():
				return fmt.Errorf("name a check or a branch, not both")
			case check:
				run, _, err = e.RunOrExecution(ctx, args[0])
			default:
				run, err = branchflags.LatestOf(ctx, e, streams, where, "name a branch or a check, or run this in a branch's worktree")
			}
			if err != nil {
				return err
			}
			artifacts, err := e.Artifacts(ctx, run)
			if err != nil {
				return err
			}
			viewed := artifactsJSON{Run: run.Name(), Artifacts: []artifactJSON{}}
			for _, artifact := range artifacts {
				viewed.Artifacts = append(viewed.Artifacts, artifactJSON{Port: artifact.Port, Platform: checks.DescribeEnvironment(artifact.Environment), Name: artifact.Name, Path: artifact.Path, Missing: artifact.Missing})
			}
			streams.Emit(viewed)
			if len(artifacts) == 0 {
				fmt.Fprintf(streams.Prose(), "%s built no archive: no port passed.\n", run.Name())
				return nil
			}
			for _, artifact := range viewed.Artifacts {
				where := s.Places().Tilde(artifact.Path)
				if artifact.Missing != "" {
					where = artifact.Missing
				}
				fmt.Fprintf(streams.Prose(), "%s  %s  %s\n", artifact.Port, artifact.Platform, where)
			}
			return nil
		},
	}
	where.Register(cmd, s, "show the archives of")
	settings.SupportsJSON(cmd)
	return cmd
}

type artifactsJSON struct {
	Run       string         `json:"run"`
	Artifacts []artifactJSON `json:"artifacts"`
}

type artifactJSON struct {
	Port     string `json:"port"`
	Platform string `json:"platform"`
	Name     string `json:"name,omitempty"`
	Path     string `json:"path,omitempty"`
	Missing  string `json:"missing,omitempty"`
}
