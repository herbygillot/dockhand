package command

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
)

func initCommand(s *settings, streams Streams) *cobra.Command {
	var worktrees string
	var yes bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up dockhand for this ports checkout; safe to rerun",
		Long: `Registers this ports checkout, finds its remote for macports/macports-ports,
and chooses where branch worktrees go: beside the clone unless --worktrees or
the configuration file says otherwise. It needs no GitHub login and no build
setup; those come when something needs them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, file, configPath, err := s.options()
			if err != nil {
				return err
			}
			e, err := engine.Open(cmd.Context(), options)
			if err != nil {
				return err
			}
			defer e.Close()
			out := streams.Out

			upstream, err := e.UpstreamRemote(cmd.Context())
			if err != nil {
				return err
			}
			if upstream != nil {
				fmt.Fprintf(out, "Using %s; upstream is %s (remote %s).\n\n", tilde(e.Clone()), engine.UpstreamRepository, upstream.Name)
			} else {
				fmt.Fprintf(out, "Using %s; it has no remote for %s, so master is fetched from %s.\n\n", tilde(e.Clone()), engine.UpstreamRepository, e.Upstream())
			}

			chosen := file.Worktrees
			switch {
			case worktrees != "":
				if chosen, err = absolute(worktrees); err != nil {
					return err
				}
			case chosen == "" && streams.terminal() && !yes:
				answer, err := ask(streams, fmt.Sprintf("Keep branch worktrees beside this clone, in %s? [Y/n] ", tilde(e.DefaultWorktrees())))
				if err != nil {
					return err
				}
				if strings.HasPrefix(strings.ToLower(answer), "n") {
					where, err := ask(streams, "Where, then? ")
					if err != nil {
						return err
					}
					if where == "" {
						return fmt.Errorf("no directory given; rerun with --worktrees <directory>")
					}
					if chosen, err = absolute(where); err != nil {
						return err
					}
				}
			}
			if chosen != "" && chosen != file.Worktrees {
				if err := config.SetWorktrees(configPath, chosen); err != nil {
					return err
				}
			}
			if chosen == "" {
				chosen = e.DefaultWorktrees()
			}
			fmt.Fprintf(out, "  Branches     worktrees in %s\n", tilde(chosen))
			if tclsh := portTclsh(); tclsh != "" {
				fmt.Fprintf(out, "  Authoring    ✓ MacPorts at %s\n", tilde(filepath.Dir(filepath.Dir(tclsh))))
			} else {
				fmt.Fprintf(out, "  Authoring    ! port-tclsh is not on PATH or in /opt/local/bin; install MacPorts to update ports\n")
			}
			fmt.Fprintf(out, "  Publishing   %s\n", publishing(cmd.Context()))
			fmt.Fprintf(out, "  Records      %s\n\n", tilde(options.Database))
			fmt.Fprintln(out, "Next: dockhand start <name>")
			return nil
		},
	}
	cmd.Flags().StringVar(&worktrees, "worktrees", "", "keep branch worktrees in this directory, and remember it")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "accept the defaults without asking")
	return cmd
}

func startCommand(s *settings, streams Streams) *cobra.Command {
	var here bool
	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start a branch from freshly fetched master",
		Long: `Creates dockhand/<name> from MacPorts' master, fetched just now, in a sparse
worktree of its own: _resources plus the ports you edit, beside your clone.
With --here, the branch is created in this checkout instead, which must
have no uncommitted changes to tracked files.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := s.open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := e.Start(cmd.Context(), engine.StartRequest{Name: args[0], Here: here})
			if err != nil {
				return err
			}
			streams.emit(map[string]any{"branch": branchRef(branch)})
			if here {
				fmt.Fprintf(streams.Out, "Created %s from master %s (fetched just now) in this checkout, and switched to it.\n", branch.Name, engine.Short(branch.Base))
				return nil
			}
			fmt.Fprintf(streams.Out, "Created %s from master %s (fetched just now)\nDirectory: %s\nNext: cd \"$(dockhand path %s)\"\n",
				branch.Name, engine.Short(branch.Base), tilde(branch.Worktree), branch.ShortName())
			return nil
		},
	}
	cmd.Flags().BoolVar(&here, "here", false, "create the branch in this checkout instead of its own worktree")
	return cmd
}

func adoptCommand(s *settings, streams Streams) *cobra.Command {
	var pr int
	cmd := &cobra.Command{
		Use:   "adopt [branch]",
		Short: "Track a branch you made, as it stands",
		Long: `Tracks an existing branch, the one checked out here unless named, without
moving or rewriting anything. Its base is where it leaves MacPorts' master,
fetched just now. A tracked branch you renamed with Git is recognized, by
its worktree or its pull request's last push, and keeps its record.

--pr <number> brings someone's macports-ports pull request into a branch of
its own, pr-<number>, to inspect and work on. It assumes no permission to
push to their branch: submit pushes there only when they let maintainers
edit and you have write access, and never rewrites their description.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := s.open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			if pr > 0 {
				if len(args) > 0 {
					return errors.New("--pr names the pull request; adopt takes a branch or --pr, not both")
				}
				return adoptPullRequest(cmd.Context(), e, streams, pr)
			}
			request := engine.AdoptRequest{}
			if len(args) == 1 {
				request.Branch = args[0]
			}
			adoption, err := e.Adopt(cmd.Context(), request)
			if err != nil {
				return err
			}
			streams.emit(map[string]any{"branch": branchRef(adoption.Branch), "already": adoption.Already, "renamed_from": adoption.Renamed, "commits": adoption.Commits, "ports": nonNil(adoption.Scope.PortNames())})
			if adoption.Already {
				fmt.Fprintf(streams.Out, "%s is already tracked.\n", adoption.Branch.Name)
				return nil
			}
			if adoption.Renamed != "" {
				fmt.Fprintf(streams.Out, "Recognized %s as %s, renamed with Git: its record, checks, and history carry over.\n", adoption.Branch.Name, adoption.Renamed)
				if pr := adoption.Branch.PullRequest; pr != nil {
					fmt.Fprintf(streams.Out, "#%d's head can't move, so submit keeps pushing to %s.\n", pr.Number, pr.Head)
				}
				return nil
			}
			fmt.Fprintf(streams.Out, "Adopted %s: %s above master %s%s.\n", adoption.Branch.Name, plural(adoption.Commits, "commit"), engine.Short(adoption.Branch.Base), describeScope(adoption.Scope))
			if adoption.Branch.Worktree == "" {
				fmt.Fprintf(streams.Out, "It is not checked out anywhere; git switch %s checks it out.\n", adoption.Branch.Name)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "bring someone's pull request, by number, into a branch of its own")
	return cmd
}

func adoptPullRequest(ctx context.Context, e *engine.Engine, streams Streams, number int) error {
	adoption, err := e.AdoptPullRequest(ctx, number)
	if err != nil {
		return err
	}
	streams.emit(map[string]any{"branch": branchRef(adoption.Branch), "already": adoption.Already, "number": number, "title": adoption.Title,
		"author": adoption.Author, "commits": adoption.Commits, "ports": nonNil(adoption.Scope.PortNames()), "maintainers_can_edit": adoption.MaintainerCanModify})
	if adoption.Already {
		fmt.Fprintf(streams.Out, "#%d is already tracked, as %s.\n", number, adoption.Branch.ShortName())
		return nil
	}
	edits := "maintainers can edit"
	if !adoption.MaintainerCanModify {
		edits = "maintainers can't push to it; suggest changes with dockhand review " + fmt.Sprint(number)
	}
	fmt.Fprintf(streams.Out, "Adopted %s: %q by @%s, %s%s; %s.\nDirectory: %s\n", adoption.Branch.ShortName(), adoption.Title, adoption.Author,
		plural(adoption.Commits, "commit"), describeScope(adoption.Scope), edits, tilde(adoption.Branch.Worktree))
	return nil
}

func pathCommand(s *settings, streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "path [branch]",
		Short: "Print a branch's directory",
		Long: `Prints the directory a tracked branch is checked out in, for cd "$(dockhand
path <branch>)" or an editor. Without a name, the branch checked out here.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := s.open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			path, err := e.Path(cmd.Context(), selector)
			if err != nil {
				return err
			}
			streams.emit(map[string]string{"path": path})
			fmt.Fprintln(streams.Out, path)
			return nil
		},
	}
}

func describeScope(scope engine.Scope) string {
	changed := scope.PortNames()
	if scope.Resources {
		changed = append(changed, "_resources")
	}
	switch len(changed) {
	case 0:
		return ", changing no ports yet"
	case 1:
		return ", changing " + changed[0]
	}
	return ", changing " + strings.Join(changed[:len(changed)-1], ", ") + " and " + changed[len(changed)-1]
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	for _, ending := range []string{"s", "x", "ch", "sh"} {
		if strings.HasSuffix(noun, ending) {
			return fmt.Sprintf("%d %ses", n, noun)
		}
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// ask puts a question on stderr and reads one line. Input that ends
// without an answer is an empty answer, which takes the default.
func ask(streams Streams, question string) (string, error) {
	fmt.Fprint(streams.Err, question)
	lines := streams.lines
	if lines == nil {
		lines = bufio.NewReader(streams.In)
	}
	line, err := lines.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func absolute(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := homeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return filepath.Abs(path)
}

func portTclsh() string {
	if path, err := exec.LookPath("port-tclsh"); err == nil {
		return path
	}
	if path, err := exec.LookPath("/opt/local/bin/port-tclsh"); err == nil {
		return path
	}
	return ""
}

// publishing says where submit's GitHub login would come from, without
// asking GitHub.
func publishing(ctx context.Context) string {
	if name := overridingToken(); name != "" {
		return "✓ GitHub token from " + name
	}
	if _, err := authStore.Get(ctx, github.CredentialKey); err == nil {
		return "✓ GitHub login in the Keychain"
	}
	return "· not set up: dockhand auth login, when you're ready to submit"
}
