package command

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
)

// testPreparer, when set, stands in for MacPorts in every engine a
// command opens.
var testPreparer func(*engine.Engine) engine.Preparer

// testForge, when set, stands in for GitHub in every engine a command
// opens.
var testForge func(*engine.Engine) engine.Forge

// branchChoice is the branch an authoring command was pointed at.
type branchChoice struct {
	branch string
	new    bool
}

func (c *branchChoice) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.branch, "branch", "", "work in this tracked branch (the dockhand/ prefix is optional)")
	cmd.Flags().BoolVar(&c.new, "new", false, "start a new branch for this, in its own worktree")
	cmd.MarkFlagsMutuallyExclusive("branch", "new")
}

func updateCommand(s *settings, streams Streams) *cobra.Command {
	var where branchChoice
	var plan, keepOld, shared bool
	cmd := &cobra.Command{
		Use:   "update <port> [version]",
		Short: "Update a port to a newer release",
		Long: `Moves a port to the newest release upstream, or the version named, and fills
in its checksums, in the branch's working files. Nothing is committed.

The branch is --branch, else the one checked out here; --new starts one.
--plan shows the edit and changes nothing.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			request := engine.UpdateRequest{Action: record.Bump, Port: args[0], KeepOldChecksums: keepOld, SharedRelease: shared, Plan: plan}
			if len(args) == 2 {
				request.Version = args[1]
			}
			return author(cmd.Context(), s, streams, where, "update", request)
		},
	}
	where.flags(cmd)
	cmd.Flags().BoolVar(&plan, "plan", false, "show the edit and change nothing")
	cmd.Flags().BoolVar(&keepOld, "keep-old-checksums", false, "refresh legacy md5 or sha1 checksums in place rather than rewriting them as rmd160, sha256, and size")
	cmd.Flags().BoolVar(&shared, "shared-release", false, "move every subport that shares the port's release")
	return cmd
}

func checksumsCommand(s *settings, streams Streams) *cobra.Command {
	var where branchChoice
	var plan, keepOld bool
	cmd := &cobra.Command{
		Use:   "checksums <port>",
		Short: "Refresh a port's checksums for the version it names",
		Long: `Fetches the port's distfiles for the version its Portfile names and writes
their checksums, in the branch's working files: what to run after editing
the version by hand. Nothing is committed.

The branch is --branch, else the one checked out here; --new starts one.
--plan shows the edit and changes nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			request := engine.UpdateRequest{Action: record.RefreshChecksums, Port: args[0], KeepOldChecksums: keepOld, Plan: plan}
			return author(cmd.Context(), s, streams, where, "checksums", request)
		},
	}
	where.flags(cmd)
	cmd.Flags().BoolVar(&plan, "plan", false, "show the edit and change nothing")
	cmd.Flags().BoolVar(&keepOld, "keep-old-checksums", false, "refresh legacy md5 or sha1 checksums in place rather than rewriting them as rmd160, sha256, and size")
	return cmd
}

// author finds the branch, makes the edit, and reports it.
func author(ctx context.Context, s *settings, streams Streams, where branchChoice, purpose string, request engine.UpdateRequest) error {
	if where.new && request.Plan {
		return fmt.Errorf("--plan changes nothing, so it starts no branch; plan in an existing one with --branch, or drop --plan")
	}
	e, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	branch, started, err := chooseBranch(ctx, e, streams, where, request.Port, purpose)
	if err != nil {
		return err
	}
	out := streams.Out
	if started {
		fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", branch.Name, engine.Short(branch.Base))
	}
	fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), tilde(branch.Worktree))
	request.Branch = branch
	update, err := e.Update(ctx, request)
	if err != nil {
		if started {
			return fmt.Errorf("%w\nKept: %s, with nothing changed", err, branch.Name)
		}
		return err
	}
	if update.Current {
		if request.Action == record.Bump {
			fmt.Fprintf(out, "%s is already at %s; nothing to change.\n", update.Port, update.After)
		} else {
			fmt.Fprintf(out, "%s %s's checksums are current; nothing to change.\n", update.Port, update.After)
		}
		return nil
	}
	if request.Action == record.Bump {
		fmt.Fprintf(out, "%s: %s → %s%s\n", update.Port, update.Before, update.After, releaseLabel(update.Release))
	} else {
		fmt.Fprintf(out, "%s %s\n", update.Port, update.After)
	}
	if request.Plan {
		fmt.Fprintf(out, "Plan, nothing changed:\n\n%s", update.Diff)
		if !strings.HasSuffix(update.Diff, "\n") {
			fmt.Fprintln(out)
		}
		return nil
	}
	what := "Updated version and checksums"
	if request.Action == record.RefreshChecksums {
		what = "Updated checksums"
	}
	if update.Distfiles > 0 {
		what += fmt.Sprintf(" (%s)", plural(update.Distfiles, "distfile"))
	}
	if update.Before.Revision != 0 && update.After.Revision == 0 {
		what += "; revision reset to 0"
	}
	fmt.Fprintf(out, "%s.\nChanged: %s\n", what, strings.Join(update.Files, ", "))
	for _, problem := range update.PatchProblems {
		fmt.Fprintf(out, "! patch %s\n", problem)
	}
	fmt.Fprintln(out, "Next: review it with git diff, then commit it")
	return nil
}

func releaseLabel(release *record.Release) string {
	switch {
	case release == nil:
		return ""
	case release.Tag != "":
		forge := release.Forge
		switch forge {
		case "github":
			forge = "GitHub"
		case "gitlab":
			forge = "GitLab"
		}
		return fmt.Sprintf("   (%s tag %s)", strings.TrimSpace(forge), release.Tag)
	case release.Archive:
		return "   (from its distfiles)"
	}
	return ""
}

// chooseBranch is the branch an authoring command works in: --branch, a
// new one with --new, or the one checked out here. With none of those, a
// terminal is asked, and a script is told the choices.
func chooseBranch(ctx context.Context, e *engine.Engine, streams Streams, where branchChoice, port, purpose string) (model.Branch, bool, error) {
	if where.branch != "" {
		branch, err := e.Resolve(ctx, where.branch)
		return branch, false, err
	}
	if where.new {
		return startFor(ctx, e, port)
	}
	branch, err := e.Current(ctx)
	if err == nil || !errors.Is(err, engine.ErrNoBranch) {
		return branch, false, err
	}
	// A branch someone made and dockhand does not track is theirs to
	// adopt; master, or no branch at all, is no context.
	if current, currentErr := e.Repo.CurrentBranch(ctx); currentErr == nil && current != "master" && current != "main" {
		return model.Branch{}, false, err
	}

	changing, err := e.BranchesChanging(ctx, port)
	if err != nil {
		return model.Branch{}, false, err
	}
	var names []string
	for _, branch := range changing {
		names = append(names, branch.ShortName())
	}
	if !streams.terminal() {
		if len(names) == 0 {
			return model.Branch{}, false, fmt.Errorf("%s is in no open branch, and this checkout is on none; start one with --new, or name one with --branch <name>", port)
		}
		return model.Branch{}, false, fmt.Errorf("%s is changed in %s; name it with --branch %s, or start another with --new", port, strings.Join(names, ", "), names[0])
	}
	name, err := e.FreeName(ctx, port)
	if err != nil {
		return model.Branch{}, false, err
	}
	switch len(changing) {
	case 0:
		fmt.Fprintf(streams.Err, "%s is in no open branch.\n", port)
		answer, err := ask(streams, fmt.Sprintf("? start %s for it? [Y/n] ", engine.BranchName(name)))
		if err != nil {
			return model.Branch{}, false, err
		}
		if answer != "" && !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			return model.Branch{}, false, errors.New("nothing changed")
		}
		return startNamed(ctx, e, name)
	case 1:
		fmt.Fprintf(streams.Err, "%s is changed in 1 open branch: %s\n", port, changing[0].Name)
		answer, err := ask(streams, fmt.Sprintf("? %s %s there, or start a new branch? [t]here / [n]ew / [q]uit ", verb(purpose), port))
		if err != nil {
			return model.Branch{}, false, err
		}
		switch strings.ToLower(answer) {
		case "t", "there":
			return changing[0], false, nil
		case "n", "new":
			return startNamed(ctx, e, name)
		}
		return model.Branch{}, false, errors.New("nothing changed")
	}
	fmt.Fprintf(streams.Err, "%s is changed in %d open branches: %s\n", port, len(changing), strings.Join(names, ", "))
	answer, err := ask(streams, fmt.Sprintf("? start %s instead? (--branch <name> picks one of them) [y/N] ", engine.BranchName(name)))
	if err != nil {
		return model.Branch{}, false, err
	}
	if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
		return model.Branch{}, false, errors.New("nothing changed")
	}
	return startNamed(ctx, e, name)
}

func verb(purpose string) string {
	if purpose == "checksums" {
		return "refresh"
	}
	return purpose
}

func startFor(ctx context.Context, e *engine.Engine, port string) (model.Branch, bool, error) {
	name, err := e.FreeName(ctx, port)
	if err != nil {
		return model.Branch{}, false, err
	}
	return startNamed(ctx, e, name)
}

func startNamed(ctx context.Context, e *engine.Engine, name string) (model.Branch, bool, error) {
	branch, err := e.Start(ctx, engine.StartRequest{Name: name})
	return branch, err == nil, err
}
