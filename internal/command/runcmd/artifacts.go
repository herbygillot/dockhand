package runcmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/term"
)

// ArtifactsCommand prints the archives a branch's latest check built, a
// line a port and platform, for otool and smoke tests by hand (field
// testing's outdated-ports session).
func ArtifactsCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "artifacts [branch]",
		Short: "Show where a check's built archives are on this Mac",
		Long: `Prints the archive each port of a branch's latest check built, a line for
each port and platform it passed on: the port, the platform, and the
archive's path on this Mac with its name, for otool or a smoke test by hand.
A port that didn't pass built none; one whose archive wasn't kept, or that
cleanup removed, says so. In a branch's worktree, with no branch named, the
branch checked out there.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
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
			run, err := branchflags.LatestOf(ctx, e, streams, where, "name a branch, or run this in a branch's worktree")
			if err != nil {
				return err
			}
			artifacts, err := e.Artifacts(ctx, run)
			if err != nil {
				return err
			}
			viewed := artifactsJSON{Run: run.Name(), Artifacts: []artifactJSON{}}
			for _, artifact := range artifacts {
				viewed.Artifacts = append(viewed.Artifacts, artifactJSON{Port: artifact.Port, Platform: engine.DescribeEnvironment(artifact.Environment), Name: artifact.Name, Path: artifact.Path, Missing: artifact.Missing})
			}
			streams.Emit(viewed)
			if len(artifacts) == 0 {
				fmt.Fprintf(streams.Out, "%s built no archive: no port passed.\n", run.Name())
				return nil
			}
			for _, artifact := range viewed.Artifacts {
				where := s.Places().Tilde(artifact.Path)
				if artifact.Missing != "" {
					where = artifact.Missing
				}
				fmt.Fprintf(streams.Out, "%s  %s  %s\n", artifact.Port, artifact.Platform, where)
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
