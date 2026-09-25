package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// workingBranch is the branch a command that changes one works on:
// --branch, else the one checked out here. It never asks.
func workingBranch(ctx context.Context, e *engine.Engine, selector string) (model.Branch, error) {
	if selector != "" {
		return e.Resolve(ctx, selector)
	}
	branch, err := e.Current(ctx)
	if errors.Is(err, engine.ErrNoBranch) {
		return model.Branch{}, fmt.Errorf("%w; name one with --branch <name>, or run this in the branch's worktree (dockhand path <name>)", err)
	}
	return branch, err
}

func tidyCommand(s *settings, streams Streams) *cobra.Command {
	var selector, message, author string
	var squash, plan, yes bool
	cmd := &cobra.Command{
		Use:   "tidy",
		Short: "Shape the branch's commits for review",
		Long: `Proposes the commits a reviewer should see: by default one per port
directory, with the subject dockhand's commands wrote or the one your own
commits give, and every uncommitted edit included. The final files are
exactly what you have; tidy never changes a file.

A plan made only of dockhand's own edits applies without review, which is
what a script gets. Anything else is shown for review first, on a terminal.
--squash --message "port: what changed" makes one commit of the whole
branch, applied as given, since the message is yours. Before rewriting, tidy keeps the old history as a checkpoint that
dockhand restore brings back.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if message != "" && !squash {
				return errors.New("--message names the one commit --squash makes; add --squash")
			}
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := workingBranch(ctx, e, selector)
			if err != nil {
				return err
			}
			proposal, err := e.PlanTidy(ctx, engine.TidyRequest{Branch: branch, Squash: squash, Message: message, Author: author})
			if err != nil {
				return err
			}
			out := streams.Out
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), describeWork(proposal))
			if proposal.Keep {
				fmt.Fprintln(out, "The commits already follow MacPorts' rules, and nothing is uncommitted; nothing to tidy.")
				writeFindings(out, proposal)
				return nil
			}
			writeTidyPlan(out, proposal)
			if plan {
				return nil
			}
			// A squash with its message given is the person's own explicit
			// plan, so it needs no review; --yes never resolves anything else.
			explicit := squash && message != ""
			if explicit || !streams.terminal() || yes && proposal.Unambiguous() {
				if !explicit && !proposal.Unambiguous() {
					return errors.New("this plan needs review before it is applied: run dockhand tidy on a terminal, or make one commit with --squash --message \"port: what changed\"")
				}
				return applyTidy(ctx, e, streams, proposal)
			}
			for {
				answer, err := ask(streams, "Review diff [d] · Edit message [e] · Apply [a] · Cancel [q]\n> ")
				if err != nil {
					return err
				}
				switch strings.ToLower(answer) {
				case "d":
					diff, err := e.Repo.DiffTrees(ctx, proposal.BaseTree, proposal.Final)
					if err != nil {
						return err
					}
					fmt.Fprint(out, string(diff))
				case "e":
					if err := editSubjects(streams, &proposal); err != nil {
						return err
					}
					writeTidyPlan(out, proposal)
				case "a":
					if blocking := proposal.Blocking(); len(blocking) > 0 {
						fmt.Fprintf(streams.Err, "Not yet: %s\n", strings.Join(blocking, "; "))
						continue
					}
					return applyTidy(ctx, e, streams, proposal)
				case "q", "":
					fmt.Fprintln(out, "Nothing changed.")
					return nil
				}
			}
		},
	}
	cmd.Flags().StringVar(&selector, "branch", "", "tidy this tracked branch")
	cmd.Flags().BoolVar(&squash, "squash", false, "make one commit of the whole branch")
	cmd.Flags().StringVar(&message, "message", "", "the message of the commit --squash makes")
	cmd.Flags().StringVar(&author, "author", "", "attribute a commit that combines several people's commits: \"Name <email>\"")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the proposed commits and change nothing")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "apply a plan made only of dockhand's own edits without asking")
	return cmd
}

func describeWork(plan engine.TidyPlan) string {
	work := plural(len(plan.History), "commit")
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

func applyTidy(ctx context.Context, e *engine.Engine, streams Streams, plan engine.TidyPlan) error {
	result, err := e.ApplyTidy(ctx, plan)
	if err != nil {
		return err
	}
	name := result.Checkpoint.Name()
	fmt.Fprintf(streams.Out, "Created %s. The files are unchanged.\nCheckpoint %s keeps the old history (dockhand restore %s).\n", plural(len(result.Commits), "commit"), name, name)
	if plan.Branch.PullRequest != nil && len(plan.History) > 0 {
		fmt.Fprintf(streams.Out, "#%d still shows %s until you submit; submit will replace its history, if no one else has pushed.\n", plan.Branch.PullRequest.Number, plural(len(plan.History), "commit"))
	}
	return nil
}

func restoreCommand(s *settings, streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <checkpoint>",
		Short: "Put back the history a tidy replaced",
		Long: `Puts a branch's commits back as they were before the tidy that made the
checkpoint, such as tidy-3, when nothing has been committed since. Files are
not touched: edits that tidy committed read as uncommitted again.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := s.open(cmd.Context())
			if err != nil {
				return err
			}
			defer e.Close()
			checkpoint, branch, err := e.Restore(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(streams.Out, "Restored %s to its history before %s (%s). The files are unchanged.\n", branch.Name, checkpoint.Name(), engine.Short(checkpoint.Before))
			return nil
		},
	}
}
