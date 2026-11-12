package checkverbs

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/checks"
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
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			// What's named is resolved once, the check or a branch's latest
			// (docs/designs/check-lifecycle.md, "Selection"): one attempt,
			// or a provider's run, is refused by name, since artifacts lists
			// a check's judged passes, some reused or filled from earlier
			// checks, never what one attempt built.
			var request engine.SelectRequest
			switch {
			case check && where.Explicit():
				return fmt.Errorf("name a check or a branch, not both")
			case len(args) == 1 && where.Explicit():
				return fmt.Errorf("name a branch once, as the argument or with -b, -p, or --pr")
			case len(args) == 1:
				// A check, an attempt or a provider run, refused below, or
				// else a branch: resolved once (the confirmation of #193's
				// fixes, A4).
				request.Name, request.Else = args[0], engine.ElseBranch
			case where.Explicit():
				branch, err := where.Resolve(ctx, e, streams)
				if err != nil {
					return err
				}
				request.Branch = &branch
			}
			selection, err := e.SelectBuild(ctx, request)
			if errors.Is(err, engine.ErrNoBranch) && request.Name == "" {
				return errors.New("name a branch or a check, or run this in a branch's worktree")
			}
			if err != nil {
				return err
			}
			switch selection.Kind {
			case engine.SelectedAttempt, engine.SelectedProviderRun:
				return fmt.Errorf("artifacts lists a check's passed builds; %s is one %s of %s. Run dockhand artifacts %s", args[0], selection.Kind, selection.Run.Name(), selection.Run.Name())
			case engine.SelectedBranch:
				// A branch named, as -b names one, says which where it isn't
				// the one checked out here.
				if here, err := e.Current(ctx); err == nil && here.ID != selection.Run.Branch && len(args) == 1 {
					if named, err := e.Branch(ctx, selection.Run.Branch); err == nil {
						fmt.Fprintf(streams.Err, "%s (not %s, checked out here)\n", named.ShortName(), here.ShortName())
					}
				}
			}
			run := selection.Run
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
