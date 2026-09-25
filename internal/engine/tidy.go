package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// TidyRequest asks what a branch's commits should be (Design v3 §8).
type TidyRequest struct {
	Branch model.Branch
	// Squash makes one commit of the whole branch, with Message.
	Squash  bool
	Message string
	// Author attributes a commit that combines several people's commits,
	// as "Name <email>".
	Author string
}

// TidyGroup is one proposed commit.
type TidyGroup struct {
	// Directory is the port directory the commit changes; empty for files
	// outside any port, or for a squash.
	Directory string
	Ports     []string
	Paths     []string
	Message   string
	// Combines are the branch's existing commits it takes changes from.
	Combines []git.HistoryCommit
	// Working is true when it includes edits not yet committed.
	Working bool
	Author  git.Signature
	// FromEdits is true when its files are exactly what dockhand's
	// authoring commands wrote, and its subject the one they wrote.
	FromEdits bool
	// Notes say why it needs a person's review.
	Notes []string
	// Blocking are what must be settled before it can be applied.
	Blocking []string
}

// Subject is the proposed message's first line.
func (g TidyGroup) Subject() string {
	subject, _, _ := strings.Cut(g.Message, "\n")
	return subject
}

// TidyPlan is a proposed series of commits for a branch, bound to the
// branch head and working files it was made from.
type TidyPlan struct {
	Branch   model.Branch
	Worktree string
	// Base and Head are commits; Final is the tree the series ends at,
	// the working files as they were captured, and BaseTree the base's.
	Base, Head, Final, BaseTree string
	History                     []git.HistoryCommit
	Groups                      []TidyGroup
	// Keep is true when the history already has a good shape and nothing
	// is uncommitted.
	Keep bool
	// Findings are problems in the files, for the person to fix; tidy
	// never changes files.
	Findings []commitrules.Finding
}

// Unambiguous reports whether the plan may be applied without review:
// every commit is one port directory's edits, exactly as dockhand's
// authoring commands wrote them, under the subject they wrote.
func (p TidyPlan) Unambiguous() bool {
	if p.Keep || len(p.Groups) == 0 {
		return false
	}
	for _, g := range p.Groups {
		if !g.FromEdits || g.Directory == "" || len(g.Notes) > 0 || len(g.Blocking) > 0 {
			return false
		}
	}
	return true
}

// Blocking lists what must be settled before the plan can be applied.
func (p TidyPlan) Blocking() []string {
	var all []string
	for _, g := range p.Groups {
		all = append(all, g.Blocking...)
	}
	return all
}

// PlanTidy proposes the commits a branch should have: one per port
// directory by default, with the subject dockhand's authoring commands
// wrote or the one the branch's own commits give, and every edit not yet
// committed included. Nothing is changed.
func (e *Engine) PlanTidy(ctx context.Context, request TidyRequest) (TidyPlan, error) {
	branch := request.Branch
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return TidyPlan{}, err
	}
	head, final, err := worktree.WorkingTree(ctx)
	if err != nil {
		return TidyPlan{}, err
	}
	base := string(branch.Base)
	if above, err := worktree.IsAncestor(ctx, base, head); err != nil {
		return TidyPlan{}, err
	} else if !above {
		return TidyPlan{}, fmt.Errorf("%s is not above its base %s; rebase it onto master first", branch.Name, short(branch.Base))
	}
	history, err := worktree.History(ctx, base, head)
	if err != nil {
		return TidyPlan{}, err
	}
	for _, commit := range history {
		if commit.Merge() {
			return TidyPlan{}, fmt.Errorf("%s has a merge commit, %s; tidy never flattens a merge by itself. Rebase onto master (git rebase %s), then tidy", branch.Name, short(model.ObjectID(commit.ID)), short(branch.Base))
		}
	}
	trees, err := worktree.CommitTrees(ctx, []string{base, head})
	if err != nil {
		return TidyPlan{}, err
	}
	plan := TidyPlan{Branch: branch, Worktree: branch.Worktree, Base: base, Head: head, Final: final, BaseTree: trees[base], History: history}
	changed, err := worktree.ChangedPaths(ctx, trees[base], final)
	if err != nil {
		return TidyPlan{}, err
	}
	if len(changed) == 0 {
		return TidyPlan{}, fmt.Errorf("%s changes nothing yet; there is nothing to tidy", branch.Name)
	}
	if plan.Findings, err = portfileFindings(ctx, worktree, trees[base], final, changed); err != nil {
		return TidyPlan{}, err
	}
	var working []string
	if trees[head] != final {
		if working, err = worktree.ChangedPaths(ctx, trees[head], final); err != nil {
			return TidyPlan{}, err
		}
	}
	if len(working) == 0 && !request.Squash && !commitrules.Errors(commitrules.CheckCommits(ruleCommits(history))) {
		plan.Keep = true
		return plan, nil
	}

	var edits []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		edits, err = r.Edits(branch.ID)
		return err
	}); err != nil {
		return TidyPlan{}, err
	}
	person, err := worktree.Author(ctx)
	if err != nil {
		return TidyPlan{}, errors.New("git has no identity to commit as; set one with git config --global user.name \"Your Name\" and git config --global user.email you@example.org")
	}
	person.When = e.now()
	author, err := parseAuthor(request.Author, person.When)
	if err != nil {
		return TidyPlan{}, err
	}

	byDirectory := map[string][]string{}
	var order []string
	for _, commit := range history {
		for _, path := range commit.Paths {
			if directory := groupOf(path); !slices.Contains(order, directory) && slices.Contains(changed, path) {
				order = append(order, directory)
			}
		}
	}
	for _, path := range changed {
		directory := groupOf(path)
		if !slices.Contains(order, directory) {
			order = append(order, directory)
		}
		byDirectory[directory] = append(byDirectory[directory], path)
	}
	// Files outside any port, such as a PortGroup, come first: the ports
	// that load them are built on them.
	if i := slices.Index(order, ""); i > 0 {
		order = append([]string{""}, slices.Delete(order, i, i+1)...)
	}

	for _, directory := range order {
		paths := byDirectory[directory]
		group := TidyGroup{Directory: directory, Paths: paths, Working: slices.ContainsFunc(paths, func(p string) bool { return slices.Contains(working, p) })}
		if directory != "" {
			group.Ports = []string{directory[strings.LastIndexByte(directory, '/')+1:]}
		}
		for _, commit := range history {
			if slices.ContainsFunc(commit.Paths, func(p string) bool { return slices.Contains(paths, p) }) {
				group.Combines = append(group.Combines, commit)
				if others := otherGroups(commit.Paths, directory); len(others) > 0 {
					group.Notes = append(group.Notes, fmt.Sprintf("commit %s %q also changes %s; its changes are split between commits", short(model.ObjectID(commit.ID)), commit.Subject(), strings.Join(others, ", ")))
				}
			}
		}
		chained, subject, err := fromEdits(ctx, worktree, trees[base], final, directory, paths, edits)
		if err != nil {
			return TidyPlan{}, err
		}
		group.FromEdits = chained
		if chained {
			for _, commit := range group.Combines {
				if body(commit.Message) != "" {
					group.Notes = append(group.Notes, fmt.Sprintf("commit %s %q has a message body that this commit would not keep", short(model.ObjectID(commit.ID)), commit.Subject()))
				}
			}
		} else if directory != "" {
			group.Notes = append(group.Notes, "has changes dockhand's commands did not make; review them")
		}
		var chosen *git.HistoryCommit
		if subject == "" {
			for i, commit := range group.Combines {
				if directory == "" || strings.HasPrefix(commit.Subject(), group.Ports[0]+":") || strings.HasPrefix(commit.Subject(), group.Ports[0]+",") {
					chosen, subject = &group.Combines[i], commit.Subject()
					break
				}
			}
			if chosen != nil {
				group.Notes = append(group.Notes, fmt.Sprintf("subject from your commit %s", short(model.ObjectID(chosen.ID))))
			} else if subject, err = derivedSubject(ctx, worktree, trees[base], final, group); err != nil {
				return TidyPlan{}, err
			} else if subject != "" {
				group.Notes = append(group.Notes, "subject from the change to its Portfile")
			} else {
				what := "the files outside any port"
				if directory != "" {
					what = directory
				}
				group.Blocking = append(group.Blocking, fmt.Sprintf("the commit for %s needs a subject: edit it, or use --squash --message", what))
			}
		}
		chosenBody := ""
		if chosen != nil {
			chosenBody = body(chosen.Message)
		}
		group.Message = composeMessage(subject, chosenBody, group.Combines, chained)
		group.Author, err = groupAuthor(group, person, author)
		if err != nil {
			return TidyPlan{}, err
		}
		if group.Author.Name == "" {
			group.Blocking = append(group.Blocking, fmt.Sprintf("the commit for %s combines commits by %s; choose the attribution with --author", describeDirectory(directory), strings.Join(authorsOf(group.Combines), " and ")))
		}
		plan.Groups = append(plan.Groups, group)
	}

	if request.Squash {
		plan.Groups = []TidyGroup{squash(plan.Groups, changed, history, request.Message, person, author)}
	}
	return plan, nil
}

// squash makes one commit of the whole branch.
func squash(groups []TidyGroup, changed []string, history []git.HistoryCommit, message string, person, author git.Signature) TidyGroup {
	group := TidyGroup{Paths: changed, Combines: history}
	for _, g := range groups {
		group.Ports = append(group.Ports, g.Ports...)
		group.Working = group.Working || g.Working
	}
	switch {
	case strings.TrimSpace(message) != "":
		subject, rest, _ := strings.Cut(strings.TrimSpace(message), "\n")
		group.Message = composeMessage(subject, strings.TrimSpace(rest), history, false)
	case len(groups) == 1 && len(groups[0].Blocking) == 0:
		group.Message = groups[0].Message
		group.Notes = append(group.Notes, "subject from the only commit it proposes")
	default:
		group.Blocking = append(group.Blocking, "a squash needs its message: --message \"port: what changed\"")
	}
	group.Author, _ = groupAuthor(group, person, author)
	if group.Author.Name == "" {
		group.Blocking = append(group.Blocking, fmt.Sprintf("the squash combines commits by %s; choose the attribution with --author", strings.Join(authorsOf(history), " and ")))
	}
	return group
}

// derivedSubject is the subject §8 gives a port's change that its files
// show plainly: a new Portfile is a new port, and a changed version an
// update to it. Anything else has none.
func derivedSubject(ctx context.Context, repo *git.Repository, before, after string, group TidyGroup) (string, error) {
	if group.Directory == "" {
		return "", nil
	}
	portfile := group.Directory + "/Portfile"
	if !slices.Contains(group.Paths, portfile) {
		return "", nil
	}
	old, oldText, err := repo.File(ctx, before, portfile)
	if err != nil {
		return "", nil
	}
	current, text, err := repo.File(ctx, after, portfile)
	if err != nil || !current.Exists {
		return "", nil
	}
	if !old.Exists {
		return group.Ports[0] + ": new port", nil
	}
	from, to := commitrules.Version(string(oldText)), commitrules.Version(string(text))
	if to != "" && from != to && !strings.Contains(to, "$") {
		return group.Ports[0] + ": update to " + to, nil
	}
	return "", nil
}

var portPath = regexp.MustCompile(`^[^._/][^/]*/[^/]+/`)

// groupOf is the port directory a path belongs to, or "" for a file
// outside any port.
func groupOf(path string) string {
	if !portPath.MatchString(path) {
		return ""
	}
	return portDirectory(path)
}

func otherGroups(paths []string, directory string) []string {
	var others []string
	for _, path := range paths {
		if g := groupOf(path); g != directory && !slices.Contains(others, describeDirectory(g)) {
			others = append(others, describeDirectory(g))
		}
	}
	return others
}

func describeDirectory(directory string) string {
	if directory == "" {
		return "files outside any port"
	}
	return directory
}

// fromEdits reports whether a port directory's changes are exactly what
// dockhand's authoring commands wrote there, one after another from the
// base, with nothing changed in between; and if so, the subject they wrote:
// the last update's, else the last edit's.
func fromEdits(ctx context.Context, repo *git.Repository, baseTree, final, directory string, paths []string, edits []model.Edit) (bool, string, error) {
	var mine []model.Edit
	for _, edit := range edits {
		if edit.Directory == directory {
			mine = append(mine, edit)
		}
	}
	if directory == "" || len(mine) == 0 {
		return false, "", nil
	}
	var touched []string
	for _, edit := range mine {
		for _, file := range edit.Files {
			if !slices.Contains(touched, file.Path) {
				touched = append(touched, file.Path)
			}
		}
	}
	for _, path := range paths {
		if !slices.Contains(touched, path) {
			return false, "", nil
		}
	}
	state, err := repo.FileBlobs(ctx, baseTree, touched)
	if err != nil {
		return false, "", err
	}
	for _, edit := range mine {
		for _, file := range edit.Files {
			if model.ObjectID(state[file.Path]) != file.Before {
				return false, "", nil
			}
		}
		for _, file := range edit.Files {
			if file.After == "" {
				delete(state, file.Path)
			} else {
				state[file.Path] = string(file.After)
			}
		}
	}
	finalBlobs, err := repo.FileBlobs(ctx, final, touched)
	if err != nil {
		return false, "", err
	}
	for _, path := range touched {
		if state[path] != finalBlobs[path] {
			return false, "", nil
		}
	}
	subject := mine[len(mine)-1].Subject
	for _, edit := range slices.Backward(mine) {
		if edit.Kind == model.EditUpdate {
			subject = edit.Subject
			break
		}
	}
	return true, subject, nil
}

var trailerLine = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*: \S`)

// body is a message's text after its subject, less its trailer paragraph.
func body(message string) string {
	_, rest, _ := strings.Cut(strings.TrimSpace(message), "\n")
	text, _ := splitTrailers(strings.TrimSpace(rest))
	return text
}

// splitTrailers separates a body's final paragraph when every line of it
// is a trailer.
func splitTrailers(text string) (string, []string) {
	paragraphs := strings.Split(text, "\n\n")
	last := strings.Split(strings.TrimSpace(paragraphs[len(paragraphs)-1]), "\n")
	for _, line := range last {
		if !trailerLine.MatchString(line) {
			return text, nil
		}
	}
	return strings.TrimSpace(strings.Join(paragraphs[:len(paragraphs)-1], "\n\n")), last
}

// composeMessage writes a subject, a body, and the trailers the combined
// commits carried: Closes:, then See:, then the others, then dockhand's
// Generated-By: when dockhand wrote the edit.
func composeMessage(subject, text string, combined []git.HistoryCommit, generated bool) string {
	var closes, see, other []string
	add := func(list *[]string, line string) {
		if !slices.Contains(*list, line) {
			*list = append(*list, line)
		}
	}
	for _, commit := range combined {
		_, rest, _ := strings.Cut(strings.TrimSpace(commit.Message), "\n")
		_, trailers := splitTrailers(strings.TrimSpace(rest))
		for _, line := range trailers {
			switch {
			case commitmsg.IsAttribution(line):
			case strings.HasPrefix(line, "Closes:"):
				add(&closes, line)
			case strings.HasPrefix(line, "See:"):
				add(&see, line)
			default:
				add(&other, line)
			}
		}
	}
	trailers := slices.Concat(closes, see, other)
	if generated {
		trailers = append(trailers, commitmsg.GeneratedBy())
	}
	parts := []string{strings.TrimSpace(subject)}
	if text = strings.TrimSpace(text); text != "" {
		parts = append(parts, text)
	}
	if len(trailers) > 0 {
		parts = append(parts, strings.Join(trailers, "\n"))
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// groupAuthor is who a proposed commit is attributed to: the person, for
// edits not yet committed; the author of the commits it combines, when
// there is one; or the one chosen. An empty name means a choice is needed.
func groupAuthor(group TidyGroup, person, chosen git.Signature) (git.Signature, error) {
	authors := authorsOf(group.Combines)
	switch {
	case len(authors) == 0:
		return person, nil
	case len(authors) == 1:
		last := group.Combines[len(group.Combines)-1].Author
		for _, commit := range group.Combines {
			if commit.Author.When.After(last.When) {
				last.When = commit.Author.When
			}
		}
		return last, nil
	case chosen.Name != "":
		return chosen, nil
	}
	return git.Signature{}, nil
}

func authorsOf(commits []git.HistoryCommit) []string {
	var authors []string
	for _, commit := range commits {
		name := commit.Author.Name + " <" + commit.Author.Email + ">"
		if !slices.Contains(authors, name) {
			authors = append(authors, name)
		}
	}
	return authors
}

func parseAuthor(value string, when time.Time) (git.Signature, error) {
	if value == "" {
		return git.Signature{}, nil
	}
	name, rest, ok := strings.Cut(value, "<")
	email, _, closed := strings.Cut(rest, ">")
	if !ok || !closed || strings.TrimSpace(name) == "" || !strings.Contains(email, "@") {
		return git.Signature{}, fmt.Errorf("--author %q should be \"Name <email>\"", value)
	}
	return git.Signature{Name: strings.TrimSpace(name), Email: strings.TrimSpace(email), When: when}, nil
}

func ruleCommits(history []git.HistoryCommit) []commitrules.Commit {
	var commits []commitrules.Commit
	for _, commit := range history {
		commits = append(commits, commitrules.Commit{ID: commit.ID, Message: commit.Message, Merge: commit.Merge(), Ports: ScopeOf(commit.Paths).PortNames()})
	}
	return commits
}

// portfileFindings applies the content rules to the Portfiles a branch
// changes.
func portfileFindings(ctx context.Context, repo *git.Repository, before, after string, changed []string) ([]commitrules.Finding, error) {
	var portfiles []commitrules.Portfile
	for _, path := range changed {
		if !strings.HasSuffix(path, "/Portfile") {
			continue
		}
		_, old, err := repo.File(ctx, before, path)
		if err != nil {
			continue
		}
		_, current, err := repo.File(ctx, after, path)
		if err != nil {
			continue
		}
		portfiles = append(portfiles, commitrules.Portfile{Path: path, Before: string(old), After: string(current)})
	}
	return commitrules.CheckPortfiles(portfiles), nil
}

// TidyResult is what applying a plan did.
type TidyResult struct {
	Checkpoint model.Checkpoint
	Commits    []string
}

// ErrStalePlan reports a plan whose branch or files changed since it was
// made.
var ErrStalePlan = errors.New("the branch changed since this plan was made")

// ApplyTidy writes the plan's commits and moves the branch to them, after
// saving a checkpoint of the old history. The final tree is the working
// files as they were planned, so every file stays as it is; the index is
// reset to the new head.
func (e *Engine) ApplyTidy(ctx context.Context, plan TidyPlan) (TidyResult, error) {
	if plan.Keep || len(plan.Groups) == 0 {
		return TidyResult{}, errors.New("the plan changes nothing")
	}
	if blocking := plan.Blocking(); len(blocking) > 0 {
		return TidyResult{}, fmt.Errorf("the plan can't be applied yet: %s", strings.Join(blocking, "; "))
	}
	worktree, err := e.worktree(ctx, plan.Branch)
	if err != nil {
		return TidyResult{}, err
	}
	head, final, err := worktree.WorkingTree(ctx)
	if err != nil {
		return TidyResult{}, err
	}
	if head != plan.Head || final != plan.Final {
		return TidyResult{}, fmt.Errorf("%w; run dockhand tidy again", ErrStalePlan)
	}
	committer, err := worktree.Author(ctx)
	if err != nil {
		return TidyResult{}, err
	}
	committer.When = e.now()
	trees, err := worktree.CommitTrees(ctx, []string{plan.Base})
	if err != nil {
		return TidyResult{}, err
	}
	parent, tree := plan.Base, trees[plan.Base]
	var result TidyResult
	for _, group := range plan.Groups {
		if tree, err = worktree.ComposeTree(ctx, tree, plan.Final, group.Paths); err != nil {
			return TidyResult{}, err
		}
		if parent, err = worktree.WriteCommit(ctx, git.Commit{Tree: tree, Parents: []string{parent}, Message: group.Message, Author: group.Author, Committer: committer}); err != nil {
			return TidyResult{}, err
		}
		result.Commits = append(result.Commits, parent)
	}
	if tree != plan.Final {
		return TidyResult{}, fmt.Errorf("engine: the tidied commits end at tree %s, not the working files' %s; nothing was changed", tree, plan.Final)
	}

	// The checkpoint's number is taken, and the refs moved, inside one
	// transaction, so two tidies cannot claim the same checkpoint.
	var checkpoint model.Checkpoint
	ref := "refs/heads/" + plan.Branch.Name
	moved := false
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		number, err := tx.NextCheckpointNumber()
		if err != nil {
			return err
		}
		checkpoint = model.Checkpoint{Number: number, Branch: plan.Branch.ID, Before: model.ObjectID(plan.Head), After: model.ObjectID(parent), At: e.now()}
		if err := worktree.UpdateRefs(ctx, []git.RefChange{
			{Name: checkpoint.Ref(), Desired: git.RefValue{Exists: true, Object: plan.Head}},
			{Name: ref, Expected: git.RefValue{Exists: true, Object: plan.Head}, Desired: git.RefValue{Exists: true, Object: parent}},
		}); err != nil {
			return fmt.Errorf("%w; nothing was changed: %w", ErrStalePlan, err)
		}
		moved = true
		if err := tx.AddCheckpoint(checkpoint); err != nil {
			return err
		}
		_, err = tx.AppendEvent(model.Event{At: checkpoint.At, Branch: plan.Branch.ID, Kind: "branch.tidy", Level: model.LevelInfo,
			Message: fmt.Sprintf("tidied %s into %s (checkpoint %s)", plural(len(plan.History), "commit"), plural(len(result.Commits), "commit"), checkpoint.Name())})
		return err
	})
	if err != nil {
		if moved {
			err = errors.Join(err, worktree.UpdateRefs(context.WithoutCancel(ctx), []git.RefChange{
				{Name: ref, Expected: git.RefValue{Exists: true, Object: parent}, Desired: git.RefValue{Exists: true, Object: plan.Head}},
				{Name: checkpoint.Ref(), Expected: git.RefValue{Exists: true, Object: plan.Head}},
			}))
		}
		return TidyResult{}, err
	}
	result.Checkpoint = checkpoint
	if err := worktree.ResetIndex(ctx); err != nil {
		return result, fmt.Errorf("the commits are made, but resetting the index failed: %w; git reset brings it in line", err)
	}
	return result, nil
}

// Restore puts a branch's history back as a tidy checkpoint kept it, when
// the branch is still where that tidy left it. The working files are not
// touched, so edits tidy had committed read as uncommitted again.
func (e *Engine) Restore(ctx context.Context, name string) (model.Checkpoint, model.Branch, error) {
	number, err := strconv.Atoi(strings.TrimPrefix(name, "tidy-"))
	if err != nil || !strings.HasPrefix(name, "tidy-") {
		return model.Checkpoint{}, model.Branch{}, fmt.Errorf("%q is not a checkpoint name, such as tidy-3", name)
	}
	var checkpoint model.Checkpoint
	var branch model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		if checkpoint, err = r.Checkpoint(number); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("there is no checkpoint %s", name)
			}
			return err
		}
		branch, err = r.Branch(checkpoint.Branch)
		return err
	}); err != nil {
		return model.Checkpoint{}, model.Branch{}, err
	}
	if checkpoint.RestoredAt != nil {
		return checkpoint, branch, fmt.Errorf("%s was already restored", name)
	}
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return checkpoint, branch, err
	}
	head, _, err := worktree.Branch(ctx, branch.Name)
	if err != nil {
		return checkpoint, branch, err
	}
	if model.ObjectID(head) != checkpoint.After {
		return checkpoint, branch, fmt.Errorf("%s has moved on since %s (it is at %s, not %s); restoring would discard that work, so nothing was changed",
			branch.Name, name, short(model.ObjectID(head)), short(checkpoint.After))
	}
	if err := worktree.UpdateRefs(ctx, []git.RefChange{{Name: "refs/heads/" + branch.Name,
		Expected: git.RefValue{Exists: true, Object: string(checkpoint.After)}, Desired: git.RefValue{Exists: true, Object: string(checkpoint.Before)}}}); err != nil {
		return checkpoint, branch, err
	}
	if err := worktree.ResetIndex(ctx); err != nil {
		return checkpoint, branch, err
	}
	restored := e.now()
	checkpoint.RestoredAt = &restored
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.MarkRestored(checkpoint); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: restored, Branch: branch.ID, Kind: "branch.restore", Level: model.LevelInfo,
			Message: fmt.Sprintf("restored %s's history from %s", branch.Name, name)})
		return err
	})
	return checkpoint, branch, err
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
