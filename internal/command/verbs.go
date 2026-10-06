package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

func editCommand(s *settings, streams Streams) *cobra.Command {
	var where branchChoice
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "edit <port>",
		Short: "Open a port's files in your editor",
		Long: `Brings a port's directory into the branch's worktree, when a sparse worktree
does not hold it yet, and opens its Portfile in $VISUAL or $EDITOR. Without
a terminal or an editor, or with --no-open, it prints the Portfile's path.

The branch is --branch, else the one checked out here; --new starts one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, started, err := chooseBranch(ctx, e, streams, where, args[0], "edit")
			if err != nil {
				return err
			}
			if started {
				fmt.Fprintf(streams.Out, "Started %s from master %s (fetched just now)\n", branch.Name, engine.Short(branch.Base))
			}
			directory, err := e.Edit(ctx, branch, args[0])
			if err != nil {
				return err
			}
			portfile := filepath.Join(branch.Worktree, filepath.FromSlash(directory), "Portfile")
			streams.emit(map[string]any{"branch": branchRef(branch), "started": started, "directory": directory, "portfile": portfile})
			editor := firstOf(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
			// --no-open only brings the port's files in, for an editor of
			// the person's own, where EDITOR=true stood in for it (field
			// testing's batch 13).
			if noOpen || !streams.terminal() || editor == "" {
				fmt.Fprintln(streams.Out, portfile)
				return nil
			}
			fmt.Fprintf(streams.Err, "%s · %s\n", branch.ShortName(), tilde(portfile))
			command := exec.CommandContext(ctx, "sh", "-c", editor+` "$1"`, "sh", portfile)
			command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := command.Run(); err != nil {
				return fmt.Errorf("the editor failed: %w", err)
			}
			fmt.Fprintln(streams.Out, "Next: "+nextIn(ctx, e, branch, "dockhand checksums "+args[0]+" (if you changed the version)", "dockhand check"))
			return nil
		},
	}
	where.flags(cmd)
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "bring the port's files into the worktree and print the Portfile's path, opening no editor")
	return cmd
}

func revbumpCommand(s *settings, streams Streams) *cobra.Command {
	var selector, subject string
	var plan bool
	cmd := &cobra.Command{
		Use:   "revbump <port>... --subject <reason>",
		Short: "Bump ports' revisions, for a rebuild",
		Long: `Increases each port's revision, so users rebuild it, and records the
reason as its commit subject for tidy: "<port>: <reason>", such as
--subject "rebuild for poppler 25.09.0". A revision shared by several
subports is bumped for all of them.

The branch is --branch, started from master where no branch has the name
yet, else the one checked out here. Otherwise a new branch is started,
since a rebuild has its own reason.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if strings.TrimSpace(subject) == "" {
				return errors.New(`revbump needs the reason as --subject, such as --subject "rebuild for poppler 25.09.0"; maintainers read it`)
			}
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, started, err := revbumpBranch(ctx, e, selector, args[0], plan)
			if err != nil {
				return err
			}
			out := streams.Out
			if started {
				fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", branch.Name, engine.Short(branch.Base))
			}
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), prose.Plural(len(args), "port"))
			width := 0
			for _, port := range args {
				width = max(width, len(port))
			}
			result := revbumpJSON{Branch: branchRef(branch), Started: started, Subject: strings.TrimSpace(subject), Applied: !plan, Ports: []revbumpedJSON{}}
			defer func() { streams.emit(result) }()
			for _, port := range args {
				update, err := e.Update(ctx, engine.UpdateRequest{Branch: branch, Action: model.EditRevbump, Port: port, Subject: subject, Plan: plan})
				if err != nil {
					return fmt.Errorf("%s: %w", port, err)
				}
				result.Ports = append(result.Ports, revbumpedJSON{Port: update.Port, Before: update.Before.Revision, After: update.After.Revision})
				fmt.Fprintf(out, "  %-*s  revision %d → %d\n", width, update.Port, update.Before.Revision, update.After.Revision)
				if plan {
					fmt.Fprint(out, update.Diff)
				}
			}
			if plan {
				fmt.Fprintln(out, "Plan, nothing changed.")
				return nil
			}
			fmt.Fprintf(out, "Recorded the subject for tidy: \"<port>: %s\"\n", strings.TrimSpace(subject))
			return nil
		},
	}
	cmd.Flags().StringVarP(&selector, "branch", "b", "", "work in this branch, by its exact name (the dockhand/ prefix is optional)")
	cmd.Flags().StringVar(&subject, "subject", "", "the reason, which becomes each commit's subject after the port's name")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the edits and change nothing")
	return cmd
}

// revbumpBranch is --branch, else the branch checked out here, else a new
// one named for the first port.
func revbumpBranch(ctx context.Context, e *engine.Engine, selector, port string, plan bool) (model.Branch, bool, error) {
	if selector != "" {
		if plan {
			branch, err := e.Resolve(ctx, selector)
			if errors.Is(err, engine.ErrNoBranch) {
				return branch, false, fmt.Errorf("--plan changes nothing, so it starts no branch, and no branch is named %s yet; without --plan, this starts it from master", selector)
			}
			return branch, false, err
		}
		return namedOrStarted(ctx, e, selector)
	}
	branch, err := e.Current(ctx)
	if err == nil || !errors.Is(err, engine.ErrNoBranch) {
		return branch, false, err
	}
	if current, currentErr := e.Repo.CurrentBranch(ctx); currentErr == nil && current != "master" && current != "main" {
		return model.Branch{}, false, err
	}
	if plan {
		return model.Branch{}, false, errors.New("--plan changes nothing, so it starts no branch; plan in an existing one with --branch")
	}
	return startFor(ctx, e, port, whatFor("revbump"))
}

func retryCommand(s *settings, streams Streams) *cobra.Command {
	var enqueue bool
	cmd := &cobra.Command{
		Use:   "retry <run>",
		Short: "Run a finished check's exact request again",
		Long: `Queues a finished check again, such as check-42: the same files, the same
plan, and the same environments, whatever the branch holds now. check
captures newer edits instead. Until results are reused port by port, the
whole plan is built again.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			previous, err := e.RunNamed(ctx, args[0])
			if err != nil {
				return err
			}
			run, err := e.Retry(ctx, previous)
			if err != nil {
				return err
			}
			revision, err := e.Revision(ctx, run.Revision)
			if err != nil {
				return err
			}
			fmt.Fprintf(streams.Out, "%s repeats %s: %s\n", run.Name(), previous.Name(), engine.Describe(revision))
			return runQueued(ctx, e, run, streams, enqueue)
		},
	}
	cmd.Flags().BoolVarP(&enqueue, "enqueue", "d", false, "queue it and return")
	return cmd
}

func rebaseCommand(s *settings, streams Streams) *cobra.Command {
	var where branchFlags
	cmd := &cobra.Command{
		Use:   "rebase",
		Short: "Move the branch's commits onto fresh master",
		Long: `Fetches MacPorts' master and replays the branch's commits on it, in the
branch's worktree, keeping the old history as a checkpoint that dockhand
restore brings back. A branch with uncommitted edits is refused, and a
rebase that conflicts is abandoned with the branch as it was.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := where.resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			rebased, err := e.Rebase(ctx, branch)
			if err != nil {
				return err
			}
			result := map[string]any{"branch": branch.ShortName(), "from": rebased.From, "to": rebased.To, "up_to_date": rebased.UpToDate, "commits": rebased.Commits}
			if rebased.Checkpoint != nil {
				result["checkpoint"] = rebased.Checkpoint.Name()
			}
			streams.emit(result)
			if rebased.UpToDate {
				fmt.Fprintf(streams.Out, "%s already starts from master %s (fetched just now).\n", branch.ShortName(), engine.Short(rebased.To))
				return nil
			}
			name := rebased.Checkpoint.Name()
			fmt.Fprintf(streams.Out, "Rebased %s (%s) from master %s onto %s.\nCheckpoint %s keeps the old history (dockhand undo %s).\n",
				branch.ShortName(), prose.Plural(rebased.Commits, "commit"), engine.Short(rebased.From), engine.Short(rebased.To), name, name)
			if len(rebased.OlderBuilds) > 0 {
				fmt.Fprintf(streams.Out, "The rebased commits keep their Generated-By, naming an older dockhand, %s; tidy names this build in a commit it writes again.\n", strings.Join(rebased.OlderBuilds, ", "))
			}
			if branch.PullRequest != nil {
				fmt.Fprintf(streams.Out, "#%d still has the old commits; dockhand submit replaces them, if no one else has pushed.\n", branch.PullRequest.Number)
			}
			// A check of the rebased files can already stand, as one does
			// after a second rebase onto the same master: status says what
			// then moves the branch on.
			moved, err := e.Branch(ctx, branch.ID)
			if err != nil {
				return err
			}
			status, err := e.BranchStatus(ctx, moved)
			if err != nil {
				return err
			}
			if status.Current {
				fmt.Fprintf(streams.Out, "%s checked these files already: %s.\n", status.Latest.Name(), checkState(status))
				writeNext(streams.Out, status)
				return nil
			}
			fmt.Fprintln(streams.Out, "Next: "+nextIn(ctx, e, branch, "dockhand check (the files it builds on have changed)"))
			return nil
		},
	}
	where.register(cmd, s, "rebase")
	return cmd
}

func archiveCommand(s *settings, streams Streams) *cobra.Command {
	var undo, keep, discard bool
	cmd := &cobra.Command{
		Use:   "archive [branch]",
		Short: "Set aside a branch you are not working on",
		Long: `Sets a branch aside: status leaves it out, and its worktree is removed
where it holds nothing the branch's commits don't. The Git branch, your
fork's branch, the checkpoints, the record, and the pull request stay, so
--undo brings it back, and the worktree is checked out again when a command
next needs it; status --all still shows it.

A worktree with uncommitted edits or untracked files is asked about on a
terminal: commit them, as tidy would, where its plan needs no words from
you; discard them; or keep the worktree. A script keeps it and says so;
--discard removes it, edits and all, and --keep-worktree keeps any worktree.
A pull request still open is said, with how to close it: archive never
closes it.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			var where branchFlags
			if len(args) == 1 {
				where.branch = args[0]
			}
			branch, err := where.resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			request := engine.ArchiveRequest{Branch: branch, Undo: undo, KeepWorktree: keep, Discard: discard}
			archived, err := e.ArchiveBranch(ctx, request)
			if err != nil {
				return err
			}
			if archived.Kept != "" && streams.terminal() {
				if archived, err = commitOrDiscard(ctx, e, streams, request, archived); err != nil {
					return err
				}
			}
			branch = archived.Branch
			streams.emit(map[string]any{"branch": branch.ShortName(), "state": branch.State, "worktree_removed": archived.Removed != "", "worktree_kept": archived.Kept})
			out := streams.Out
			if undo {
				fmt.Fprintf(out, "%s is back among your open branches.\n", branch.ShortName())
				return nil
			}
			switch {
			case archived.Removed != "":
				fmt.Fprintf(out, "Archived %s, and removed its worktree; its Git branch and pull request stay. dockhand archive --undo %s brings it back.\n", branch.ShortName(), branch.ShortName())
			case archived.Kept != "":
				fmt.Fprintf(out, "Archived %s; its worktree stays, since %s. dockhand archive --discard %s removes it, edits and all.\n", branch.ShortName(), archived.Kept, branch.ShortName())
			default:
				fmt.Fprintf(out, "Archived %s; its files, Git branch, and pull request are untouched. dockhand archive --undo %s brings it back.\n", branch.ShortName(), branch.ShortName())
			}
			if pr := branch.PullRequest; pr != nil && archived.PullRequestOpen {
				fmt.Fprintf(out, "#%d is still open; archive doesn't close it. Close it on GitHub, or with: gh pr close %d --repo %s\n", pr.Number, pr.Number, pr.Repository)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "bring an archived branch back")
	cmd.Flags().BoolVar(&keep, "keep-worktree", false, "keep the worktree")
	cmd.Flags().BoolVar(&discard, "discard", false, "remove the worktree though it has uncommitted edits or untracked files, which go")
	cmd.MarkFlagsMutuallyExclusive("keep-worktree", "discard")
	return cmd
}

// commitOrDiscard asks what to do with the edits a worktree being
// archived holds: commit them, where tidy's plan needs no words from the
// person; discard them; or keep the worktree.
func commitOrDiscard(ctx context.Context, e *engine.Engine, streams Streams, request engine.ArchiveRequest, archived engine.Archived) (engine.Archived, error) {
	fmt.Fprintf(streams.Err, "%s's worktree stays for now: %s.\n", archived.Branch.ShortName(), archived.Kept)
	answer, err := ask(streams, "? commit them, discard them, or keep the worktree? [c]ommit / [d]iscard / [K]eep ")
	if err != nil {
		return archived, err
	}
	request.Branch = archived.Branch
	switch strings.ToLower(answer) {
	case "c", "commit":
		plan, err := e.PlanTidy(ctx, engine.TidyRequest{Branch: archived.Branch})
		if err != nil {
			return archived, err
		}
		if !plan.Unambiguous() {
			fmt.Fprintf(streams.Err, "Its commits need your words; dockhand tidy -b %s shapes them, and archive takes the worktree after.\n", archived.Branch.ShortName())
			return archived, nil
		}
		if _, err := e.ApplyTidy(ctx, plan); err != nil {
			return archived, err
		}
		return e.ArchiveBranch(ctx, request)
	case "d", "discard":
		request.Discard = true
		return e.ArchiveBranch(ctx, request)
	}
	return archived, nil
}
