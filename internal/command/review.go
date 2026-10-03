package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

func tidyCommand(s *settings, streams Streams) *cobra.Command {
	var where branchFlags
	var message, author, group, saveTo, apply string
	var squash, plan, yes bool
	cmd := &cobra.Command{
		Use:   "tidy",
		Short: "Shape the branch's commits for review",
		Long: `Proposes the commits a reviewer should see: by default one per port
directory, with the subject dockhand's commands wrote or the one your own
commits give, and every uncommitted edit included. The final files are
exactly what you have; tidy never changes a file.

A plan made only of dockhand's own edits applies without review, which is
what a script gets. Anything else is shown for review first, on a terminal,
or applied as shown with --yes.
--squash --message "port: what changed" makes one commit of the whole
branch, applied as given, since the message is yours. Before rewriting,
tidy keeps the old history as a checkpoint that dockhand undo brings
back.

--group rearranges the proposed commits: "2 1+3" makes commit 2 first, then
one commit of 1 and 3. On a terminal, [g] does the same.

--plan --out <file> saves the plan for review; edit its messages there if
you like. --apply <file> applies it, as long as the branch's base, its
commits, and its files are as they were when it was saved.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if message != "" && !squash {
				return errors.New("--message names the one commit --squash makes; add --squash")
			}
			if saveTo != "" && !plan {
				return errors.New("--out saves the plan --plan shows; add --plan")
			}
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if apply != "" {
				return applySaved(ctx, e, streams, apply)
			}
			branch, err := where.resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			proposal, err := e.PlanTidy(ctx, engine.TidyRequest{Branch: branch, Squash: squash, Message: message, Author: author})
			if err != nil {
				return err
			}
			if group != "" && !proposal.Keep {
				if proposal, err = e.Regroup(ctx, proposal, group, author); err != nil {
					return fmt.Errorf("--group: %w", err)
				}
			}
			streams.emit(tidyView(proposal))
			out := streams.Out
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), describeWork(proposal))
			if proposal.Keep && len(proposal.Warnings) > 0 {
				// What the rules warn of is said, as submit says it, with
				// the plan to rewrite a message in (the rust and cargo run).
				fmt.Fprintln(out, "The commits follow MacPorts' rules, and nothing is uncommitted, but for what the rules warn of:")
				for _, warning := range proposal.Warnings {
					fmt.Fprintf(out, "  %s\n", warning)
				}
				writeFindings(out, proposal)
				if !plan || saveTo == "" {
					fmt.Fprintln(out, "To rewrite a message: dockhand tidy --plan --out tidy.toml, edit it there, then dockhand tidy --apply tidy.toml")
					return nil
				}
				data, err := proposal.Save()
				if err != nil {
					return err
				}
				if err := os.WriteFile(saveTo, data, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(out, "Saved the commits as they are to %s. Edit their messages there, then: dockhand tidy --apply %s\n", saveTo, saveTo)
				return nil
			}
			if proposal.Keep {
				fmt.Fprintln(out, "The commits already follow MacPorts' rules, and nothing is uncommitted; nothing to tidy.")
				writeFindings(out, proposal)
				return nil
			}
			writeTidyPlan(out, proposal)
			if plan {
				if saveTo == "" {
					return nil
				}
				data, err := proposal.Save()
				if err != nil {
					return err
				}
				if err := os.WriteFile(saveTo, data, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(out, "Saved the plan to %s. Edit its messages there if you like, then: dockhand tidy --apply %s\n", saveTo, saveTo)
				return nil
			}
			// A squash with its message given, or a grouping chosen, is the
			// person's own explicit plan, so it needs no review.
			_, err = decideTidy(ctx, e, streams, proposal, squash && message != "" || group != "", yes, author)
			return err
		},
	}
	where.register(cmd, s, "Tidy")
	cmd.Flags().BoolVar(&squash, "squash", false, "make one commit of the whole branch")
	cmd.Flags().StringVar(&message, "message", "", "the message of the commit --squash makes")
	cmd.Flags().StringVar(&author, "author", "", "attribute a commit that combines several people's commits: \"Name <email>\"")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the proposed commits and change nothing")
	cmd.Flags().StringVar(&group, "group", "", "rearrange the proposed commits: \"2 1+3\" puts 2 first, then 1 and 3 as one")
	cmd.Flags().StringVar(&saveTo, "out", "", "with --plan, save the plan to this file")
	cmd.Flags().StringVar(&apply, "apply", "", "apply a plan saved with --plan --out")
	cmd.MarkFlagsMutuallyExclusive("apply", "plan")
	cmd.MarkFlagsMutuallyExclusive("apply", "squash")
	cmd.MarkFlagsMutuallyExclusive("apply", "group")
	cmd.MarkFlagsMutuallyExclusive("apply", "branch")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "apply the plan as shown, without asking; a plan made only of dockhand's own edits applies without it")
	return cmd
}

// decideTidy applies a tidy plan, or on a terminal reviews it first. An
// explicit plan, the person's own, applies as given; without a terminal,
// or with --yes, a plan made only of dockhand's own edits applies too,
// and anything else needs review. It reports whether the plan applied.
func decideTidy(ctx context.Context, e *engine.Engine, streams Streams, proposal engine.TidyPlan, explicit, yes bool, author string) (bool, error) {
	out := streams.Out
	// --yes applies the plan as shown, whosever its edits are, as --yes
	// does elsewhere: a hand edit's plan tidy proposed itself had no way
	// to be taken without a terminal (field testing's seventh report).
	if explicit || !streams.terminal() || yes {
		if !explicit && !yes && !proposal.Unambiguous() {
			return false, errors.New("this plan needs review before it is applied: review it on a terminal, apply it as shown with --yes, or make one commit with --squash --message \"port: what changed\"")
		}
		if blocking := proposal.Blocking(); len(blocking) > 0 {
			return false, fmt.Errorf("nothing was applied: %s", strings.Join(blocking, "; "))
		}
		return true, applyTidy(ctx, e, streams, proposal)
	}
	for {
		answer, err := ask(streams, "Review diff [d] · Change groups [g] · Edit message [e] · Apply [a] · Cancel [q]\n> ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "d":
			diff, err := e.Repo.DiffTrees(ctx, proposal.BaseTree, proposal.Final)
			if err != nil {
				return false, err
			}
			fmt.Fprint(out, string(diff))
		case "g":
			spec, err := ask(streams, "Commits in order, joining any to combine with + (such as 2 1+3): ")
			if err != nil {
				return false, err
			}
			regrouped, err := e.Regroup(ctx, proposal, spec, author)
			if err != nil {
				fmt.Fprintf(streams.Err, "Not changed: %v\n", err)
				continue
			}
			proposal = regrouped
			writeTidyPlan(out, proposal)
		case "e":
			if err := editSubjects(streams, &proposal); err != nil {
				return false, err
			}
			writeTidyPlan(out, proposal)
		case "a":
			if blocking := proposal.Blocking(); len(blocking) > 0 {
				fmt.Fprintf(streams.Err, "Not yet: %s\n", strings.Join(blocking, "; "))
				continue
			}
			return true, applyTidy(ctx, e, streams, proposal)
		case "q", "":
			fmt.Fprintln(out, "Nothing changed.")
			return false, nil
		}
	}
}

func describeWork(plan engine.TidyPlan) string {
	work := prose.Plural(len(plan.History), "commit")
	uncommitted := false
	for _, group := range plan.Groups {
		uncommitted = uncommitted || group.Working
	}
	switch {
	case uncommitted && len(plan.History) == 0:
		return "edits not yet committed"
	case uncommitted:
		return work + " and edits not yet committed"
	}
	return work
}

func writeTidyPlan(out io.Writer, plan engine.TidyPlan) {
	title := "Proposed commit"
	if len(plan.Groups) > 1 {
		title = "Proposed series"
	}
	fmt.Fprintf(out, "\n%s\n", title)
	for i, group := range plan.Groups {
		subject := group.Subject()
		if subject == "" {
			subject = "(needs a subject)"
		}
		fmt.Fprintf(out, "  %d  %s\n", i+1, subject)
		if len(group.Combines) > 0 {
			var subjects []string
			for _, commit := range group.Combines {
				subjects = append(subjects, strconv.Quote(commit.Subject()))
			}
			fmt.Fprintf(out, "       combines %s\n", strings.Join(subjects, ", "))
		}
		if group.Working {
			fmt.Fprintln(out, "       includes edits not yet committed")
		}
		if group.Created {
			fmt.Fprintln(out, "       a new port create wrote, with your edits since")
		}
		fmt.Fprintf(out, "       files: %s\n", strings.Join(group.Paths, ", "))
		if _, rest, ok := strings.Cut(strings.TrimSpace(group.Message), "\n\n"); ok {
			for _, line := range strings.Split(rest, "\n") {
				if strings.HasPrefix(line, "Closes:") || strings.HasPrefix(line, "See:") {
					fmt.Fprintf(out, "       keeps: %s\n", line)
				}
			}
		}
		if group.Author.Name != "" && len(group.Combines) > 0 {
			fmt.Fprintf(out, "       author: %s <%s>\n", group.Author.Name, group.Author.Email)
		}
		for _, note := range group.Notes {
			fmt.Fprintf(out, "       · %s\n", note)
		}
		for _, blocking := range group.Blocking {
			fmt.Fprintf(out, "       ✗ %s\n", blocking)
		}
	}
	if builds := plan.OlderBuilds(); len(builds) > 0 {
		fmt.Fprintf(out, "  · the branch's commits name an older dockhand in Generated-By, %s: a commit tidy leaves as it is keeps it, and one it writes names this build\n", strings.Join(builds, ", "))
	}
	writeFindings(out, plan)
	fmt.Fprintln(out)
}

// writeSavedPlan says what applying a saved plan writes: each commit's
// whole message, as it may have been edited, with its files and author.
func writeSavedPlan(out io.Writer, plan engine.TidyPlan) {
	title := "Commit to write"
	if len(plan.Groups) > 1 {
		title = "Commits to write"
	}
	fmt.Fprintf(out, "\n%s\n", title)
	for i, group := range plan.Groups {
		subject, body, _ := strings.Cut(strings.TrimSpace(group.Message), "\n")
		if subject == "" {
			subject = "(needs a subject)"
		}
		fmt.Fprintf(out, "  %d  %s\n", i+1, subject)
		if group.Working {
			fmt.Fprintln(out, "       includes edits not yet committed")
		}
		fmt.Fprintf(out, "       files: %s\n", strings.Join(group.Paths, ", "))
		if group.Author.Name != "" {
			fmt.Fprintf(out, "       author: %s <%s>\n", group.Author.Name, group.Author.Email)
		}
		if body = strings.TrimSpace(body); body != "" {
			fmt.Fprintln(out, "       body:")
			for _, line := range strings.Split(body, "\n") {
				if strings.TrimSpace(line) == "" {
					fmt.Fprintln(out)
					continue
				}
				fmt.Fprintf(out, "         %s\n", line)
			}
		}
		for _, blocking := range group.Blocking {
			fmt.Fprintf(out, "       ✗ %s\n", blocking)
		}
	}
	writeFindings(out, plan)
	fmt.Fprintln(out)
}

func writeFindings(out io.Writer, plan engine.TidyPlan) {
	if len(plan.Findings) == 0 {
		return
	}
	fmt.Fprintln(out, "Also noticed, for you to fix (tidy never changes files):")
	for _, finding := range plan.Findings {
		fmt.Fprintf(out, "  %s\n", finding)
	}
}

// editSubjects asks for each commit's subject, keeping the rest of its
// message.
func editSubjects(streams Streams, plan *engine.TidyPlan) error {
	for i := range plan.Groups {
		group := &plan.Groups[i]
		answer, err := ask(streams, fmt.Sprintf("Subject for commit %d [%s]: ", i+1, group.Subject()))
		if err != nil {
			return err
		}
		if answer == "" {
			continue
		}
		_, rest, _ := strings.Cut(group.Message, "\n")
		group.Message = answer + "\n" + rest
		if rest == "" {
			group.Message = answer + "\n"
		}
		var kept []string
		for _, blocking := range group.Blocking {
			if !strings.Contains(blocking, "needs a subject") {
				kept = append(kept, blocking)
			}
		}
		group.Blocking = kept
		group.FromEdits = false
	}
	return nil
}

// applySaved applies a plan saved with --plan --out. Saving it was the
// review, so it is applied as it stands.
func applySaved(ctx context.Context, e *engine.Engine, streams Streams, file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	proposal, err := e.LoadTidyPlan(ctx, data)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	streams.emit(tidyView(proposal))
	fmt.Fprintf(streams.Out, "%s · the plan saved in %s\n", proposal.Branch.ShortName(), file)
	writeSavedPlan(streams.Out, proposal)
	if blocking := proposal.Blocking(); len(blocking) > 0 {
		return fmt.Errorf("the plan can't be applied yet: %s", strings.Join(blocking, "; "))
	}
	return applyTidy(ctx, e, streams, proposal)
}

func applyTidy(ctx context.Context, e *engine.Engine, streams Streams, plan engine.TidyPlan) error {
	result, err := e.ApplyTidy(ctx, plan)
	if err != nil {
		return err
	}
	applied := tidyView(plan)
	applied.Applied = &tidyAppliedJSON{Checkpoint: result.Checkpoint.Name(), Commits: result.Commits, Kept: result.Kept}
	streams.emit(applied)
	name := result.Checkpoint.Name()
	made := fmt.Sprintf("Created %s", prose.Plural(len(result.Commits), "commit"))
	if result.Kept > 0 {
		as := "they were"
		if result.Kept == 1 {
			as = "it was"
		}
		made = fmt.Sprintf("Kept %s as %s, and created %d", prose.Plural(result.Kept, "commit"), as, len(result.Commits)-result.Kept)
	}
	fmt.Fprintf(streams.Out, "%s. The files are unchanged.\nCheckpoint %s keeps the old history (dockhand undo %s).\n", made, name, name)
	if len(result.Narrowed) > 0 {
		fmt.Fprintf(streams.Out, "Left %s out of the worktree again, as nothing of the branch's is there.\n", strings.Join(result.Narrowed, ", "))
	}
	if warning := modifiedBuildWarning(plan, name); warning != "" {
		fmt.Fprintln(streams.Err, warning)
	}
	switch {
	case plan.Branch.PullRequest == nil || len(plan.History) == 0:
	case result.Kept == len(plan.History):
		// Every commit it has stays, so submitting adds the rest to it.
		fmt.Fprintf(streams.Out, "#%d gains %s when you submit, on top of the %s it has.\n", plan.Branch.PullRequest.Number, prose.Plural(len(result.Commits)-result.Kept, "commit"), prose.Plural(len(plan.History), "commit"))
	default:
		fmt.Fprintf(streams.Out, "#%d still shows %s until you submit; submit will replace its history, if no one else has pushed.\n", plan.Branch.PullRequest.Number, prose.Plural(len(plan.History), "commit"))
	}
	return nil
}

// modifiedBuildWarning says, for a tidy whose commits name a dockhand
// built from uncommitted source, how to name one anybody can find before
// they're submitted.
func modifiedBuildWarning(plan engine.TidyPlan, checkpoint string) string {
	if !plan.ModifiedBuild() {
		return ""
	}
	return fmt.Sprintf("! Generated-By names this dockhand, built from uncommitted source, which nobody else can find. Before submitting, tidy again with a build of a pushed commit: dockhand undo %s, then dockhand tidy.", checkpoint)
}

// restoreCommand is undo's former name, kept for scripts: it takes the
// checkpoint by its name.
func restoreCommand(s *settings, streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:    "restore <checkpoint>",
		Short:  "Put back the history a tidy or rebase replaced (now dockhand undo)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := s.open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			return restoreCheckpoint(cmd.Context(), e, streams, args[0])
		},
	}
}

// undoCommand puts back what a tidy or rebase replaced: the branch's
// latest, or the checkpoint named (the command-line UX review's smaller
// items: git restore restores files, and dockhand's restores history).
func undoCommand(s *settings, streams Streams) *cobra.Command {
	var where branchFlags
	cmd := &cobra.Command{
		Use:   "undo [<checkpoint>]",
		Short: "Put back the history the latest tidy or rebase replaced",
		Long: `Puts a branch's commits back as they were before its latest tidy or
rebase, or the checkpoint named, such as tidy-3 or rebase-4, when nothing
has been committed since. After a tidy, the files are not touched: edits
that tidy committed read as uncommitted again. After a rebase, the files go
back as they were before it, with the master the branch started from; a
change to one of those files stops it.

With no checkpoint named, the branch is -b, -p, or --pr, else the one
checked out here.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if len(args) == 1 {
				if where.explicit() {
					return errors.New("a checkpoint names its branch already; name the checkpoint or the branch, not both")
				}
				return restoreCheckpoint(ctx, e, streams, args[0])
			}
			branch, err := where.resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			name, err := e.LatestCheckpoint(ctx, branch)
			if err != nil {
				return err
			}
			return restoreCheckpoint(ctx, e, streams, name)
		},
	}
	where.register(cmd, s, "Undo in")
	return cmd
}

// restoreCheckpoint puts a branch back as it was before a checkpoint, and
// says what it did.
func restoreCheckpoint(ctx context.Context, e *engine.Engine, streams Streams, name string) error {
	checkpoint, branch, err := e.Restore(ctx, name)
	if err != nil {
		return err
	}
	streams.emit(map[string]any{"checkpoint": checkpoint.Name(), "branch": branch.ShortName(), "head": checkpoint.Before, "base": branch.Base})
	if checkpoint.Kind != model.CheckpointRebase {
		fmt.Fprintf(streams.Out, "Restored %s to its history before %s (%s). The files are unchanged.\n", branch.Name, checkpoint.Name(), engine.Short(checkpoint.Before))
		return nil
	}
	fmt.Fprintf(streams.Out, "Restored %s to its history and files before %s (%s)", branch.Name, checkpoint.Name(), engine.Short(checkpoint.Before))
	if checkpoint.BaseBefore != "" {
		fmt.Fprintf(streams.Out, ", on master %s again.\n", engine.Short(branch.Base))
		return nil
	}
	// A rebase recorded before checkpoints kept the base leaves the
	// branch's base where the rebase moved it, which the history put
	// back doesn't start from.
	fmt.Fprintf(streams.Out, ".\n%s didn't record the master it moved %s from, so dockhand still takes it to start from master %s. dockhand rebase puts its commits there.\n",
		checkpoint.Name(), branch.Name, engine.Short(branch.Base))
	return nil
}
