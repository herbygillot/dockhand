package view

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/prose"
)

func DescribeWork(plan engine.TidyPlan) string {
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

// writeTidyPlan lists a tidy plan's commits: as proposed, with what each
// combines, keeps, and notes; or, saved, what applying it writes, each
// commit's whole message as it may have been edited, with its author.
func WriteTidyPlan(out io.Writer, plan engine.TidyPlan, saved bool) {
	title := map[bool]string{false: "Proposed commit", true: "Commit to write"}[saved]
	if len(plan.Groups) > 1 {
		title = map[bool]string{false: "Proposed series", true: "Commits to write"}[saved]
	}
	fmt.Fprintf(out, "\n%s\n", title)
	for i, group := range plan.Groups {
		subject, body, _ := strings.Cut(strings.TrimSpace(group.Message), "\n")
		if !saved {
			// A proposal's first line is its subject, empty or not.
			subject = group.Subject()
		}
		if subject == "" {
			subject = "(needs a subject)"
		}
		fmt.Fprintf(out, "  %d  %s\n", i+1, subject)
		if len(group.Combines) > 0 && !saved {
			var subjects []string
			for _, commit := range group.Combines {
				subjects = append(subjects, strconv.Quote(commit.Subject()))
			}
			fmt.Fprintf(out, "       combines %s\n", strings.Join(subjects, ", "))
		}
		if group.Working {
			fmt.Fprintln(out, "       includes edits not yet committed")
		}
		if group.Created && !saved {
			fmt.Fprintln(out, "       new port from dockhand create, including your later edits")
		}
		fmt.Fprintf(out, "       files: %s\n", strings.Join(group.Paths, ", "))
		if !saved {
			if _, rest, ok := strings.Cut(strings.TrimSpace(group.Message), "\n\n"); ok {
				for _, line := range strings.Split(rest, "\n") {
					if strings.HasPrefix(line, "Closes:") || strings.HasPrefix(line, "See:") {
						fmt.Fprintf(out, "       keeps: %s\n", line)
					}
				}
			}
		}
		if group.Author.Name != "" && (saved || len(group.Combines) > 0) {
			fmt.Fprintf(out, "       author: %s <%s>\n", group.Author.Name, group.Author.Email)
		}
		if body = strings.TrimSpace(body); body != "" && saved {
			fmt.Fprintln(out, "       body:")
			for _, line := range strings.Split(body, "\n") {
				if strings.TrimSpace(line) == "" {
					fmt.Fprintln(out)
					continue
				}
				fmt.Fprintf(out, "         %s\n", line)
			}
		}
		if !saved {
			for _, note := range group.Notes {
				fmt.Fprintf(out, "       · %s\n", note)
			}
		}
		for _, blocking := range group.Blocking {
			fmt.Fprintf(out, "       ✗ %s\n", blocking)
		}
	}
	if builds := plan.OlderBuilds(); len(builds) > 0 && !saved {
		fmt.Fprintf(out, "    the branch's commits name an older dockhand in Generated-By, %s: a commit tidy leaves as it is keeps it, and one it writes names this build\n", strings.Join(builds, ", "))
	}
	WriteFindings(out, plan)
	fmt.Fprintln(out)
}

func WriteFindings(out io.Writer, plan engine.TidyPlan) {
	writeUntracked(out, plan)
	if len(plan.Findings) == 0 {
		return
	}
	fmt.Fprintln(out, "File issues to resolve separately (tidy only changes commits):")
	for _, finding := range plan.Findings {
		fmt.Fprintf(out, "  %s\n", finding)
	}
}

// writeUntracked says the untracked files a plan takes in and leaves out.
func writeUntracked(out io.Writer, plan engine.TidyPlan) {
	if len(plan.Taken) > 0 {
		fmt.Fprintf(out, "Taken in, untracked, since patchfiles names them: %s\n", strings.Join(plan.Taken, ", "))
	}
	if len(plan.LeftOut) > 0 {
		fmt.Fprintf(out, "Left out, since Git doesn't track them (git add one to commit it): %s\n", strings.Join(plan.LeftOut, ", "))
	}
	for _, path := range plan.Ignored {
		fmt.Fprintf(out, "Left out, though patchfiles names it, since Git ignores it as git add would: %s; git check-ignore -v %s names the rule\n", path, path)
	}
}

// modifiedBuildWarning says, for a tidy whose commits name a dockhand
// built from uncommitted source, how to name one anybody can find before
// they're submitted.
func ModifiedBuildWarning(plan engine.TidyPlan, checkpoint string) string {
	if !plan.ModifiedBuild() {
		return ""
	}
	return fmt.Sprintf("! Generated-By names this dockhand, built from uncommitted source, which nobody else can find. Before submitting, tidy again with a build of a pushed commit: dockhand undo %s, then dockhand tidy.", checkpoint)
}
