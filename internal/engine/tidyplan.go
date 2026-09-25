package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
)

// Regroup rearranges a plan's commits (Design v3 §8's "Change groups"):
// spec lists the new commits in order, each one or more of the plan's
// numbered commits joined by "+", such as "2 1+3". Every commit is named
// once. A commit made of several takes the first one's message, and
// author, when their authors differ, the one given as "Name <email>".
func (p TidyPlan) Regroup(spec, author string) (TidyPlan, error) {
	chosen, err := parseAuthor(author, time.Time{})
	if err != nil {
		return p, err
	}
	fields := strings.FieldsFunc(spec, func(r rune) bool { return r == ' ' || r == ',' })
	if len(fields) == 0 {
		return p, errors.New("name the commits in order, joining any to combine with +, such as \"2 1+3\"")
	}
	used := map[int]bool{}
	var groups []TidyGroup
	for _, field := range fields {
		var members []TidyGroup
		var numbers []int
		for _, part := range strings.Split(field, "+") {
			n, err := strconv.Atoi(part)
			if err != nil || n < 1 || n > len(p.Groups) {
				return p, fmt.Errorf("%q is not one of the commits, 1 to %d", part, len(p.Groups))
			}
			if used[n] {
				return p, fmt.Errorf("commit %d is named twice", n)
			}
			used[n] = true
			members = append(members, p.Groups[n-1])
			numbers = append(numbers, n)
		}
		if len(members) == 1 {
			groups = append(groups, members[0])
			continue
		}
		groups = append(groups, p.combine(members, numbers, chosen))
	}
	for n := 1; n <= len(p.Groups); n++ {
		if !used[n] {
			return p, fmt.Errorf("commit %d is left out; every commit is named once", n)
		}
	}
	p.Groups = groups
	return p, nil
}

// combine makes one commit of several of the plan's.
func (p TidyPlan) combine(members []TidyGroup, numbers []int, chosen git.Signature) TidyGroup {
	var group TidyGroup
	var ids []string
	for _, member := range members {
		for _, port := range member.Ports {
			if !slices.Contains(group.Ports, port) {
				group.Ports = append(group.Ports, port)
			}
		}
		group.Paths = append(group.Paths, member.Paths...)
		group.Working = group.Working || member.Working
		for _, commit := range member.Combines {
			ids = append(ids, commit.ID)
		}
	}
	slices.Sort(group.Paths)
	for _, commit := range p.History {
		if slices.Contains(ids, commit.ID) {
			group.Combines = append(group.Combines, commit)
			var elsewhere []string
			for _, path := range commit.Paths {
				if !slices.Contains(group.Paths, path) && slices.ContainsFunc(p.Groups, func(g TidyGroup) bool { return slices.Contains(g.Paths, path) }) {
					elsewhere = append(elsewhere, path)
				}
			}
			if len(elsewhere) > 0 {
				group.Notes = append(group.Notes, fmt.Sprintf("commit %s %q also changes %s; its changes are split between commits", short(model.ObjectID(commit.ID)), commit.Subject(), strings.Join(elsewhere, ", ")))
			}
		}
	}
	joined := make([]string, len(numbers))
	for i, n := range numbers {
		joined[i] = strconv.Itoa(n)
	}
	for i, member := range members {
		if member.Subject() != "" {
			group.Message = member.Message
			group.Notes = append(group.Notes, fmt.Sprintf("combines commits %s; message from commit %d, so check it says what all of them do", strings.Join(joined, ", "), numbers[i]))
			break
		}
	}
	if group.Message == "" {
		group.Blocking = append(group.Blocking, fmt.Sprintf("the commit combining %s needs a subject", strings.Join(joined, ", ")))
	}
	switch authors := authorsOf(group.Combines); {
	case len(authors) == 0:
		group.Author = members[0].Author
	default:
		group.Author, _ = groupAuthor(group, git.Signature{}, chosen)
		if group.Author.Name == "" {
			group.Blocking = append(group.Blocking, fmt.Sprintf("the commit combining %s combines commits by %s; choose the attribution with --author", strings.Join(joined, ", "), strings.Join(authors, " and ")))
		}
	}
	return group
}

// TidyPlanVersion is the version of the saved tidy plan format.
const TidyPlanVersion = 1

// savedTidyPlan is a tidy plan as a file: the commits to make, bound to
// the branch's base, its tip, and its working files when the plan was
// made. Messages and authors may be edited in the file; the paths are
// checked against the branch when it is applied.
type savedTidyPlan struct {
	Version int    `json:"version"`
	Branch  string `json:"branch"`
	Base    string `json:"base"`
	Head    string `json:"head"`
	Working string `json:"working_tree"`
	// Index is the index's tree when the plan was made; absent from plans
	// saved before it was recorded.
	Index   string        `json:"index,omitempty"`
	Commits []savedCommit `json:"commits"`
}

type savedCommit struct {
	Message  string      `json:"message"`
	Author   savedAuthor `json:"author"`
	Paths    []string    `json:"paths"`
	Combines []string    `json:"combines,omitempty"`
	Working  bool        `json:"includes_uncommitted_edits,omitempty"`
	Notes    []string    `json:"notes,omitempty"`
}

type savedAuthor struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	When  time.Time `json:"date"`
}

// Save writes the plan as a file a person can review, edit, and apply
// with LoadTidyPlan.
func (p TidyPlan) Save() ([]byte, error) {
	if p.Keep || len(p.Groups) == 0 {
		return nil, errors.New("the plan changes nothing")
	}
	saved := savedTidyPlan{Version: TidyPlanVersion, Branch: p.Branch.Name, Base: p.Base, Head: p.Head, Working: p.Final, Index: p.Index}
	for _, group := range p.Groups {
		commit := savedCommit{Message: group.Message, Author: savedAuthor(group.Author), Paths: group.Paths, Working: group.Working, Notes: group.Notes}
		for _, combined := range group.Combines {
			commit.Combines = append(commit.Combines, combined.ID)
		}
		saved.Commits = append(saved.Commits, commit)
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	return append(data, '\n'), err
}

// LoadTidyPlan reads a saved plan back, as long as the branch's base, its
// tip, and its working files are as they were when it was made, and its
// commits still cover exactly the branch's changes.
func (e *Engine) LoadTidyPlan(ctx context.Context, data []byte) (TidyPlan, error) {
	var saved savedTidyPlan
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&saved); err != nil {
		return TidyPlan{}, fmt.Errorf("this is not a saved tidy plan: %w", err)
	}
	if saved.Version != TidyPlanVersion {
		return TidyPlan{}, fmt.Errorf("this plan's format is version %d; this dockhand reads version %d", saved.Version, TidyPlanVersion)
	}
	branch, err := e.Resolve(ctx, saved.Branch)
	if err != nil {
		return TidyPlan{}, err
	}
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return TidyPlan{}, err
	}
	head, final, err := worktree.WorkingTree(ctx)
	if err != nil {
		return TidyPlan{}, err
	}
	switch {
	case string(branch.Base) != saved.Base:
		return TidyPlan{}, fmt.Errorf("%w: its base moved from %s to %s; make a new plan with dockhand tidy --plan --out", ErrStalePlan, short(model.ObjectID(saved.Base)), short(branch.Base))
	case head != saved.Head:
		return TidyPlan{}, fmt.Errorf("%w: it has new commits; make a new plan with dockhand tidy --plan --out", ErrStalePlan)
	case final != saved.Working:
		return TidyPlan{}, fmt.Errorf("%w: its files were edited; make a new plan with dockhand tidy --plan --out", ErrStalePlan)
	}
	if saved.Index != "" {
		index, err := worktree.IndexTree(ctx)
		if err != nil {
			return TidyPlan{}, err
		}
		if index != saved.Index {
			return TidyPlan{}, fmt.Errorf("%w: something was staged since it was made; make a new plan with dockhand tidy --plan --out", ErrStalePlan)
		}
	}
	history, err := worktree.History(ctx, saved.Base, head)
	if err != nil {
		return TidyPlan{}, err
	}
	trees, err := worktree.CommitTrees(ctx, []string{saved.Base})
	if err != nil {
		return TidyPlan{}, err
	}
	plan := TidyPlan{Branch: branch, Worktree: branch.Worktree, Base: saved.Base, Head: head, Final: final, BaseTree: trees[saved.Base], History: history, Index: saved.Index}
	changed, err := worktree.ChangedPaths(ctx, plan.BaseTree, final)
	if err != nil {
		return TidyPlan{}, err
	}
	var covered []string
	for i, commit := range saved.Commits {
		group := TidyGroup{Message: strings.TrimSpace(commit.Message) + "\n", Author: git.Signature(commit.Author), Paths: commit.Paths, Working: commit.Working, Notes: commit.Notes}
		for _, path := range commit.Paths {
			if slices.Contains(covered, path) {
				return TidyPlan{}, fmt.Errorf("%s is in more than one commit", path)
			}
			covered = append(covered, path)
			if directory := groupOf(path); directory != "" && !slices.Contains(group.Ports, directoryName(directory)) {
				group.Ports = append(group.Ports, directoryName(directory))
			}
		}
		for _, id := range commit.Combines {
			at := slices.IndexFunc(history, func(c git.HistoryCommit) bool { return c.ID == id })
			if at < 0 {
				return TidyPlan{}, fmt.Errorf("commit %d combines %s, which is not on the branch", i+1, short(model.ObjectID(id)))
			}
			group.Combines = append(group.Combines, history[at])
		}
		if group.Subject() == "" {
			group.Blocking = append(group.Blocking, fmt.Sprintf("commit %d needs a subject", i+1))
		}
		if group.Author.Name == "" || !strings.Contains(group.Author.Email, "@") {
			group.Blocking = append(group.Blocking, fmt.Sprintf("commit %d needs an author with a name and an email", i+1))
		}
		plan.Groups = append(plan.Groups, group)
	}
	slices.Sort(covered)
	if !slices.Equal(covered, changed) {
		return TidyPlan{}, fmt.Errorf("the plan's commits don't cover exactly the branch's changes: %s", pathDifference(changed, covered))
	}
	if plan.Findings, err = portfileFindings(ctx, worktree, plan.BaseTree, final, changed); err != nil {
		return TidyPlan{}, err
	}
	return plan, nil
}

// pathDifference words what one list of paths has that another lacks.
func pathDifference(want, have []string) string {
	var missing, extra []string
	for _, path := range want {
		if !slices.Contains(have, path) {
			missing = append(missing, path)
		}
	}
	for _, path := range have {
		if !slices.Contains(want, path) {
			extra = append(extra, path)
		}
	}
	var words []string
	if len(missing) > 0 {
		words = append(words, "missing "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		words = append(words, "not changed by the branch: "+strings.Join(extra, ", "))
	}
	return strings.Join(words, "; ")
}
