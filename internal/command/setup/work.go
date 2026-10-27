package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/term"
)

// setupCommand registers the checkout, reports dockhand's health, and on
// a terminal offers what's missing (the command-line UX review's §6): one
// first-run command, rerun for the health check, in place of init,
// providers, and auth.
func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var worktrees string
	var yes bool
	cmd := &cobra.Command{
		Use: "setup",
		// v0.3.0's spellings point here, not run as aliases (the UX
		// thread's answer on the hidden aliases, 2026-10-15).
		SuggestFor: []string{"init", "auth", "providers"},
		Short:      "Set up dockhand for this ports checkout, and report its health; safe to rerun",
		Long: `Registers this ports checkout, finds its remote for macports/macports-ports,
and chooses where branch worktrees go: ~/Source/macports-branches unless
--worktrees or the configuration file says otherwise. Then it reports what
dockhand runs with: Git, MacPorts, the places a check can build, the GitHub
login, and its records.

On a terminal it offers what's missing, each in turn and each declined by
answering no: the GitHub login, and the maintainers line the ports naming
your GitHub login write, which it writes to the configuration file. The
Tart image is offered last, since making one takes a large download;
setup tart makes it any time. Rerun, it's the health check.

It first checks the Git dockhand runs, git on PATH or $GIT_BIN, and refuses
one older than 2.40, which rebase needs.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetup(cmd, s, streams, worktrees, yes)
		},
	}
	cmd.Flags().StringVar(&worktrees, "worktrees", "", "keep branch worktrees in this directory, and remember it")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "accept the defaults, and offer nothing")
	cmd.AddCommand(setupTartCommand(s, streams), setupGitHubCommand(s, streams))
	settings.SupportsJSON(cmd)
	return cmd
}

// runSetup registers the checkout and reports, and on a terminal, unless
// yes, offers what's missing.
func runSetup(cmd *cobra.Command, s *settings.Settings, streams term.Streams, worktrees string, yes bool) error {
	options, file, configPath, err := s.Options(cmd.Context())
	if err != nil {
		return err
	}
	named := ""
	if worktrees != "" {
		if named, err = s.Places().Absolute(worktrees); err != nil {
			return err
		}
	}
	e, found, err := engine.Setup(cmd.Context(), options, named)
	report := setupReport{SetupReport: found, Providers: []string{}}
	defer func() { streams.Emit(report) }()
	if err != nil {
		return err
	}
	defer e.Close()
	out := streams.Out
	if remote := report.Upstream.Remote; remote != "" {
		fmt.Fprintf(out, "Using %s; upstream is %s (remote %s).\n\n", s.Places().Tilde(report.Checkout), report.Upstream.Repository, remote)
	} else {
		fmt.Fprintf(out, "Using %s; it has no remote for %s, so master is fetched from %s.\n\n", s.Places().Tilde(report.Checkout), report.Upstream.Repository, report.Upstream.FetchedFrom)
	}
	if report.Unchosen && streams.Terminal() && !yes {
		answer, err := streams.Ask(fmt.Sprintf("Keep branch worktrees in %s? [Y/n] ", s.Places().Tilde(report.Worktrees)))
		if err != nil {
			return err
		}
		if strings.HasPrefix(strings.ToLower(answer), "n") {
			where, err := streams.Ask("Where, then? ")
			if err != nil {
				return err
			}
			if where == "" {
				return fmt.Errorf("no directory given; rerun with --worktrees <directory>")
			}
			directory, err := s.Places().Absolute(where)
			if err != nil {
				return err
			}
			report.Choose(directory)
		}
	}
	if report.Remember != "" {
		if err := config.SetWorktrees(configPath, report.Remember); err != nil {
			return err
		}
	}
	// Each row's text wraps under its column (part 5, item 4: the Tart
	// line was "one big run-on", 2026-10-21).
	field := func(label, text string) {
		fmt.Fprintln(out, streams.Hanging(fmt.Sprintf("  %-12s ", label), text))
	}
	field("Git", fmt.Sprintf("✓ %s at %s", report.Git.Version, s.Places().Tilde(report.Git.Path)))
	field("Branches", "worktrees in "+s.Places().Tilde(report.Worktrees))
	if report.MacPorts != "" {
		field("Authoring", "✓ MacPorts at "+s.Places().Tilde(report.MacPorts))
	} else {
		field("Authoring", "! port-tclsh is not on PATH or in /opt/local/bin; install MacPorts to update ports")
	}
	for i, tool := range report.Vendoring {
		label := ""
		if i == 0 {
			label = "Vendoring"
		}
		if tool.Path != "" {
			field(label, fmt.Sprintf("✓ %s at %s, for %s", tool.Name, s.Places().Tilde(tool.Path), tool.For))
		} else {
			field(label, fmt.Sprintf("! %s is not on PATH; %s need it, as create and update write them: sudo port install %s", tool.Name, tool.For, tool.Name))
		}
	}
	for i, line := range providerLines(cmd.Context(), s, file) {
		report.Providers = append(report.Providers, line)
		label := ""
		if i == 0 {
			label = "Providers"
		}
		field(label, line)
	}
	var publishLine string
	publishLine, report.GitHub = publishing(cmd.Context(), s)
	field("Publishing", publishLine)
	field("Records", s.Places().Tilde(report.Records))
	fmt.Fprintln(out)
	if streams.Terminal() && !yes {
		written, err := offerSetup(cmd.Context(), s, e, streams, file, configPath)
		if err != nil {
			return err
		}
		if written != "" {
			file.Maintainer = written
		}
	}
	// A Next: line names only what would be accepted (the release bar's
	// rule): outdated --mine needs the maintainers line, which A10's run
	// of setup found missing.
	// The setup still to do comes first.
	first := ""
	if steps := missingSteps(cmd.Context(), s); len(steps) > 0 {
		first = strings.Join(steps, ", then ") + ", then "
	}
	if file.Maintainer != "" {
		fmt.Fprintln(out, "Next: "+first+"dockhand outdated --mine, or dockhand update <port>")
	} else {
		fmt.Fprintln(out, "Next: "+first+"dockhand update <port>, or set maintainer in the configuration file for dockhand outdated --mine")
	}
	return nil
}

func StartCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var here bool
	var ports []string
	cmd := &cobra.Command{
		Use:   "start [<name>] [--port <port>]...",
		Short: "Start a branch from freshly fetched master",
		Long: `Creates dockhand/<name> from MacPorts' master, fetched just now, in a sparse
worktree of its own: _resources plus the ports you edit, beside your clone.
--port brings a port's directory in from the start, as edit does, and can
be given more than once; with no name, the branch is named for the first
port, with a short ID, as an edit's is. With --here, the branch is created
in this checkout instead, which must have no uncommitted changes to
tracked files.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if len(args) == 0 && len(ports) == 0 {
				return errors.New("start needs a branch name or --port <port> to generate one.")
			}
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			name := ""
			if len(args) == 1 {
				name = args[0]
			} else if name, err = e.FreeName(ctx, ports[0]); err != nil {
				return err
			}
			branch, err := e.Start(ctx, engine.StartRequest{Name: name, Here: here, Ports: ports})
			// An archived branch's name is offered back: renamed aside, the
			// archived one keeps its work under <name>-archived.
			if taken := new(engine.TakenError); errors.As(err, &taken) && taken.Branch.State == model.BranchArchived && taken.Branch.PullRequest == nil && streams.Terminal() {
				ok, _, askErr := streams.Decide(false, term.Decision{Question: fmt.Sprintf("? %s is archived. Rename the archived branch to free this name? [y/N] ", taken.Branch.ShortName()), Undone: "nothing was started"})
				if askErr != nil {
					return askErr
				}
				if ok {
					renamed, renameErr := e.RenameArchived(ctx, taken.Branch)
					if renameErr != nil {
						return renameErr
					}
					fmt.Fprintf(streams.Out, "Renamed the archived %s to %s; dockhand archive --undo %s brings it back.\n", taken.Branch.ShortName(), renamed.ShortName(), renamed.ShortName())
					branch, err = e.Start(ctx, engine.StartRequest{Name: name, Here: here, Ports: ports})
				}
			}
			if err != nil {
				return err
			}
			for _, port := range ports {
				if _, err := e.Edit(ctx, branch, port); err != nil {
					return fmt.Errorf("%s was started, but %w", branch.Name, err)
				}
			}
			streams.Emit(map[string]any{"branch": view.BranchRef(branch)})
			if here {
				fmt.Fprintf(streams.Out, "Created %s from master %s (fetched just now) in this checkout, and switched to it.\n", branch.Name, branch.Base.Short())
				return nil
			}
			fmt.Fprintf(streams.Out, "Created %s from master %s (fetched just now)\nDirectory: %s\nNext: cd \"$(dockhand path %s)\"\n",
				branch.Name, branch.Base.Short(), s.Places().Tilde(branch.Worktree), branch.ShortName())
			return nil
		},
	}
	cmd.Flags().BoolVar(&here, "here", false, "create the branch in this checkout instead of its own worktree")
	cmd.Flags().StringArrayVar(&ports, "port", nil, "bring this port's directory into the worktree; more than one may be given")
	settings.SupportsJSON(cmd)
	return cmd
}

func AdoptCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var pr int
	cmd := &cobra.Command{
		Use:   "adopt [branch]",
		Short: "Track an existing branch without changing it",
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
			e, err := s.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			if pr > 0 {
				if len(args) > 0 {
					return errors.New("--pr names the pull request; adopt takes a branch or --pr, not both")
				}
				return adoptPullRequest(cmd.Context(), e, streams, s.Places(), pr)
			}
			request := engine.AdoptRequest{}
			if len(args) == 1 {
				request.Branch = args[0]
			}
			adoption, err := e.Adopt(cmd.Context(), request)
			if err != nil {
				return err
			}
			streams.Emit(map[string]any{"branch": view.BranchRef(adoption.Branch), "already": adoption.Already, "renamed_from": adoption.Renamed, "commits": adoption.Commits, "ports": view.NonNil(adoption.Scope.PortNames()),
				"uncommitted": view.NonNil(adoption.Uncommitted), "placed": adoption.Placed, "unplaced": adoption.Unplaced})
			if adoption.Already {
				fmt.Fprintf(streams.Out, "%s is already tracked.\n", adoption.Branch.Name)
				writePlaced(streams.Out, adoption)
				return nil
			}
			if adoption.Renamed != "" {
				fmt.Fprintf(streams.Out, "Recognized %s as a Git rename of %s; retained its record, checks, and history.\n", adoption.Branch.Name, adoption.Renamed)
				if pr := adoption.Branch.PullRequest; pr != nil {
					fmt.Fprintf(streams.Out, "Pull request #%d keeps its original source branch; submit will continue pushing to %s.\n", pr.Number, pr.Head)
				}
				return nil
			}
			fmt.Fprintf(streams.Out, "Adopted %s: %s above master %s%s.\n", adoption.Branch.Name, prose.Plural(adoption.Commits, "commit"), adoption.Branch.Base.Short(), describeScope(adoption.Scope, adoption.Uncommitted))
			writePlaced(streams.Out, adoption)
			return nil
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "bring someone's pull request, by number, into a branch of its own")
	settings.SupportsJSON(cmd)
	return cmd
}

// writePlaced says where adopting checked a branch out that wasn't
// anywhere, or found it checked out, or why it couldn't.
func writePlaced(out io.Writer, adoption engine.Adoption) {
	switch {
	case adoption.Placed != "" && adoption.Branch.Managed:
		fmt.Fprintf(out, "Created a worktree for this branch at %s.\n", adoption.Placed)
	case adoption.Placed != "":
		fmt.Fprintf(out, "Recorded the existing worktree at %s.\n", adoption.Placed)
	case adoption.Unplaced != "":
		fmt.Fprintf(out, "It is not checked out anywhere: %s.\n", adoption.Unplaced)
	}
}

func adoptPullRequest(ctx context.Context, e *engine.Engine, streams term.Streams, places config.Places, number int) error {
	adoption, err := e.AdoptPullRequest(ctx, number)
	if err != nil {
		return err
	}
	fmt.Fprintln(streams.Err, view.ExperimentalWords(number, adoption.Author))
	streams.Emit(map[string]any{"branch": view.BranchRef(adoption.Branch), "already": adoption.Already, "number": number, "title": adoption.Title,
		"author": adoption.Author, "commits": adoption.Commits, "ports": view.NonNil(adoption.Scope.PortNames()), "maintainers_can_edit": adoption.MaintainerCanModify})
	if adoption.Already {
		fmt.Fprintf(streams.Out, "#%d is already tracked, as %s.\n", number, adoption.Branch.ShortName())
		return nil
	}
	edits := "maintainers can edit"
	if !adoption.MaintainerCanModify {
		edits = "maintainers can't push to it; suggest changes with dockhand review " + fmt.Sprint(number)
	}
	fmt.Fprintf(streams.Out, "Adopted %s: %q by @%s, %s%s; %s.\nDirectory: %s\n", adoption.Branch.ShortName(), adoption.Title, adoption.Author,
		prose.Plural(adoption.Commits, "commit"), describeScope(adoption.Scope, nil), edits, places.Tilde(adoption.Branch.Worktree))
	return nil
}

func PathCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "path [branch]",
		Short: "Print a branch's directory",
		Long: `Prints the directory a tracked branch is checked out in, for cd "$(dockhand
path <branch>)" or an editor; -p and --pr name the branch by its port or
its pull request, and -b by its exact name, as the argument does. Without
any, the branch checked out here.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := s.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			var path string
			switch {
			case len(args) == 1 && where.Explicit():
				return errors.New("Choose the branch with either an argument or one of -b, -p, or --pr.")
			case where.Explicit():
				branch, err := where.Resolve(cmd.Context(), e, streams)
				if err != nil {
					return err
				}
				path, err = e.BranchPath(cmd.Context(), branch)
				if err != nil {
					return err
				}
			default:
				selector := ""
				if len(args) == 1 {
					selector = args[0]
				}
				if path, err = e.Path(cmd.Context(), selector); err != nil {
					return err
				}
			}
			streams.Emit(map[string]string{"path": path})
			fmt.Fprintln(streams.Out, path)
			return nil
		},
	}
	where.Register(cmd, s, "print the directory of")
	settings.SupportsJSON(cmd)
	return cmd
}

func describeScope(scope engine.Scope, uncommitted []string) string {
	changed := scope.Changed()
	words := ", changing " + prose.And(changed)
	switch {
	case len(changed) == 0 && len(uncommitted) > 0:
		return ", with edits not yet committed to " + prose.And(uncommitted)
	case len(changed) == 0:
		return ", changing no ports yet"
	case len(uncommitted) > 0:
		words += ", with edits not yet committed to " + prose.And(uncommitted)
	}
	return words
}

// setupReport is what setup reports, as --json gives it: the engine's
// findings, the places a check can build, and the GitHub login.
type setupReport struct {
	engine.SetupReport
	Providers []string        `json:"providers"`
	GitHub    githubLoginJSON `json:"github"`
}

// githubLoginJSON is the GitHub login submit would use: its account and
// where its token comes from, as GitHub was asked, until when dockhand's
// own login renews itself, or why there's none to use.
type githubLoginJSON struct {
	Account     string     `json:"account,omitempty"`
	Source      string     `json:"source,omitempty"`
	RenewsUntil *time.Time `json:"renews_until,omitempty"`
	Problem     string     `json:"problem,omitempty"`
}

// publishing says whose GitHub login submit would use, asked of GitHub,
// and where its token comes from, with the same as JSON: GH_TOKEN or
// GITHUB_TOKEN, dockhand's login in the Keychain, or the GitHub CLI's.
func publishing(ctx context.Context, s *settings.Settings) (string, githubLoginJSON) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, err := s.Host.AuthAPI(s.Signin()).LoginStatus(ctx)
	switch {
	case err == nil:
		login := githubLoginJSON{Account: status.Account, Source: string(status.Source)}
		line := "✓ " + status.Account + ", from " + status.Source.Words()
		if !status.RenewsUntil.IsZero() {
			login.RenewsUntil = &status.RenewsUntil
			line += ", renewing itself until " + status.RenewsUntil.Local().Format("2 January 2006")
		}
		return line, login
	case errors.Is(err, github.ErrLoginEnded):
		// The login's own words, and what signs in again, as its producer
		// gives them (R3-38: the words were cut out of the error).
		ended := refusal.Of(err)
		line := "! " + ended.Words()
		if len(ended.Next) > 0 {
			line += "; run " + strings.Join(view.RenderSteps(ended.Next), ", then ")
		}
		return line, githubLoginJSON{Problem: ended.Words()}
	case errors.Is(err, github.ErrNoCredentials):
		return "  not set up: dockhand setup github, when you're ready to submit", githubLoginJSON{Problem: "not set up"}
	}
	problem := "Could not identify the GitHub account: " + err.Error()
	return "! " + problem, githubLoginJSON{Problem: problem}
}
