package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/history"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
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
	// Created is a new port this branch's create wrote, edited by hand
	// since, as create asks: its edits are the port's, and create's
	// subject still names the commit, though it isn't dockhand's alone.
	Created bool
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
	// Index is the index's tree when the plan was made. Applying refuses
	// if it has changed, since something was staged that the plan never
	// saw, and the checkpoint keeps it (Design v3 §8).
	Index   string
	History []git.HistoryCommit
	Groups  []TidyGroup
	// Keep is true when the history already has a good shape and nothing
	// is uncommitted.
	Keep bool
	// Findings are problems in the files, for the person to fix; tidy
	// never changes files.
	Findings []commitrules.Finding
	// Warnings are what MacPorts' commit rules warn of in the commits as
	// they are, as submit says them: a plan that would keep the commits
	// can still be saved, for their messages to be rewritten.
	Warnings []commitrules.Finding
}

// Unambiguous reports whether the plan may be applied without review:
// every commit is one port directory's edits, exactly as dockhand's
// authoring commands wrote them, under the subject they wrote.
func (p TidyPlan) Unambiguous() bool {
	if p.Keep || len(p.Groups) == 0 {
		return false
	}
	for _, g := range p.Groups {
		if !g.FromEdits && !g.Created || g.Directory == "" || len(g.Notes) > 0 || len(g.Blocking) > 0 {
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
	index, err := worktree.IndexTree(ctx)
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
	plan := TidyPlan{Branch: branch, Worktree: branch.Worktree, Base: base, Head: head, Final: final, BaseTree: trees[base], History: history, Index: index}
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
	// Commits that break no rule are kept, but a warning is said, as
	// submit says it, with their plan to rewrite the message from: tidy
	// said "nothing to tidy" where submit warned of a body line over 72
	// characters, and saved no plan to fix it in (the rust and cargo run).
	rules := commitrules.CheckCommits(e.ruleCommits(ctx, model.Source{Commit: model.ObjectID(base), Base: model.ObjectID(base), Tree: model.ObjectID(trees[base])}, history))
	if len(working) == 0 && !request.Squash && !commitrules.Errors(rules) {
		plan.Keep = true
		if len(rules) == 0 {
			return plan, nil
		}
		plan.Warnings = rules
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

	// A directory's commit names the subports its change record says the
	// files change, terraform-1.16 where the directory is terraform's.
	records, err := e.revisionChanges(ctx, branch.ID, model.ObjectID(base), model.ObjectID(final), true)
	if err != nil {
		return TidyPlan{}, err
	}
	for _, directory := range order {
		paths := byDirectory[directory]
		group := TidyGroup{Directory: directory, Paths: paths, Working: slices.ContainsFunc(paths, func(p string) bool { return slices.Contains(working, p) })}
		if directory != "" {
			group.Ports, _ = recordedPorts([]string{directory}, records)
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
		// A new port create wrote, and the person has edited since, as
		// create asks ("Next: dockhand edit txt"), is the port: the plan
		// stands, with create's subject, where it had wanted the same
		// words typed (the txt run's finding 3). It's no longer only what
		// dockhand wrote, so it carries no attribution.
		if !chained && directory != "" {
			if subject, group.Created, err = createdPort(ctx, worktree, trees[base], directory, edits); err != nil {
				return TidyPlan{}, err
			}
		}
		if chained {
			for _, commit := range group.Combines {
				if body(commit.Message) != "" {
					group.Notes = append(group.Notes, fmt.Sprintf("commit %s %q has a message body that this commit would not keep", short(model.ObjectID(commit.ID)), commit.Subject()))
				}
			}
		} else if directory != "" && !group.Created {
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
			standing := ""
			if chosen == nil {
				if standing, err = standingSubject(ctx, worktree, final, directory, edits); err != nil {
					return TidyPlan{}, err
				}
			}
			if chosen != nil {
				whose := "your"
				if commitmsg.Attributed(chosen.Message) {
					whose = "dockhand's"
				}
				group.Notes = append(group.Notes, fmt.Sprintf("subject from %s commit %s", whose, short(model.ObjectID(chosen.ID))))
			} else if standing != "" {
				subject = standing
				group.Notes = append(group.Notes, "subject from dockhand's edits, which the other changes leave standing")
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
	// Kept for its warnings, the plan is the commits as they are, to save
	// and rewrite a message in; one that would change them is a proposal.
	if plan.Keep && !reproduces(plan.Groups, history) {
		plan.Keep = false
	}

	if request.Squash {
		plan.Groups = []TidyGroup{squash(plan.Groups, changed, history, request.Message, person, author)}
	}
	return plan, nil
}

// reproduces reports whether a plan's commits are the branch's own, one
// for one, under the same messages.
func reproduces(groups []TidyGroup, history []git.HistoryCommit) bool {
	if len(groups) != len(history) {
		return false
	}
	for i, group := range groups {
		if len(group.Combines) != 1 || group.Combines[0].ID != history[i].ID || group.Message != history[i].Message {
			return false
		}
	}
	return true
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
	file := group.Directory + "/Portfile"
	if !slices.Contains(group.Paths, file) {
		return "", nil
	}
	old, oldText, err := repo.File(ctx, before, file)
	if err != nil {
		return "", nil
	}
	current, text, err := repo.File(ctx, after, file)
	if err != nil || !current.Exists {
		return "", nil
	}
	if !old.Exists {
		return group.Ports[0] + ": new port", nil
	}
	// The version the port declares, as its Portfile proves it: the
	// group's first port, which is the directory's own or one of its
	// subports.
	subport := ""
	if group.Ports[0] != path.Base(group.Directory) {
		subport = group.Ports[0]
	}
	from, known := portfile.DeclaredVersion(oldText, subport)
	to, ok := portfile.DeclaredVersion(text, subport)
	if ok && (!known || from != to) {
		return group.Ports[0] + ": update to " + to, nil
	}
	return "", nil
}

// groupOf is the port directory a path belongs to, or "" for a file
// outside any port.
func groupOf(path string) string {
	directory, _ := macports.PortDirectoryOf(path)
	return directory
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
// chainSubject is the subject a chain of dockhand's edits of one directory
// gives: a new port's names it new, and an update's names the version,
// whatever edits followed them.
func chainSubject(mine []model.Edit) string {
	for _, kind := range []model.EditKind{model.EditCreate, model.EditUpdate} {
		for _, edit := range slices.Backward(mine) {
			if edit.Kind == kind {
				return edit.Subject
			}
		}
	}
	return mine[len(mine)-1].Subject
}

// standingSubject is the subject dockhand's edits of a directory give,
// while every line they wrote still stands in the final files: a person's
// changes beside them, as to the same Portfile, leave the update what the
// commit does. Once a line they wrote is gone, as when the person took the
// version back, it gives none.
func standingSubject(ctx context.Context, repo *git.Repository, final, directory string, edits []model.Edit) (string, error) {
	var mine []model.Edit
	for _, edit := range edits {
		if edit.Directory == directory {
			mine = append(mine, edit)
		}
	}
	if directory == "" || len(mine) == 0 {
		return "", nil
	}
	first, last := map[string]model.ObjectID{}, map[string]model.ObjectID{}
	var paths []string
	for _, edit := range mine {
		for _, file := range edit.Files {
			if _, seen := first[file.Path]; !seen {
				first[file.Path] = file.Before
				paths = append(paths, file.Path)
			}
			last[file.Path] = file.After
		}
	}
	finals, err := repo.FileBlobs(ctx, final, paths)
	if err != nil {
		return "", err
	}
	lines := func(blob model.ObjectID) (map[string]bool, error) {
		found := map[string]bool{}
		if blob == "" {
			return found, nil
		}
		data, err := repo.ReadBlob(ctx, string(blob))
		for _, line := range strings.Split(string(data), "\n") {
			found[line] = true
		}
		return found, err
	}
	for _, path := range paths {
		before, err := lines(first[path])
		if err != nil {
			return "", err
		}
		written, err := lines(last[path])
		if err != nil {
			return "", err
		}
		now, err := lines(model.ObjectID(finals[path]))
		if err != nil {
			return "", err
		}
		for line := range written {
			if !before[line] && !now[line] {
				return "", nil
			}
		}
	}
	return chainSubject(mine), nil
}

// createdPort is the subject of a port this branch's create wrote, where
// its directory is one create recorded writing and its Portfile isn't in
// the base: a new port, whoever has edited it since.
func createdPort(ctx context.Context, repo *git.Repository, baseTree, directory string, edits []model.Edit) (string, bool, error) {
	i := slices.IndexFunc(edits, func(edit model.Edit) bool { return edit.Kind == model.EditCreate && edit.Directory == directory })
	if i < 0 {
		return "", false, nil
	}
	file, _, err := repo.File(ctx, baseTree, directory+"/Portfile")
	if err != nil || file.Exists {
		return "", false, err
	}
	return edits[i].Subject, true, nil
}

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
	subject := chainSubject(mine)
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

// ruleCommits are a branch's commits as the commit rules read them, each
// with the ports its directories define at base, as base's index names
// them, so a subject may name the subport it changes.
func (e *Engine) ruleCommits(ctx context.Context, base model.Source, history []git.HistoryCommit) []commitrules.Commit {
	var directories []string
	for _, commit := range history {
		for _, directory := range macports.ScopeOf(commit.Paths).Ports {
			if !slices.Contains(directories, directory) {
				directories = append(directories, directory)
			}
		}
	}
	defined := e.portsDefined(ctx, base, directories)
	var commits []commitrules.Commit
	for _, commit := range history {
		scope := macports.ScopeOf(commit.Paths)
		var names []string
		for _, directory := range scope.Ports {
			names = append(names, defined[directory]...)
		}
		commits = append(commits, commitrules.Commit{ID: commit.ID, Message: commit.Message, Merge: commit.Merge(), Ports: scope.PortNames(), Defined: names})
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
	// Kept is how many of the leading commits are the branch's own, as
	// they were, since tidy wouldn't change them.
	Kept int
	// Narrowed are the directories a worktree dockhand made was narrowed
	// to leave out again, once nothing of the branch's is in them.
	Narrowed []string
}

// OlderBuilds are the builds the branch's commits name in Generated-By
// other than this one: a commit tidy leaves as it is keeps its line, and
// one it writes names this build.
func (p TidyPlan) OlderBuilds() []string {
	var messages []string
	for _, commit := range p.History {
		messages = append(messages, commit.Message)
	}
	return commitmsg.OtherBuilds(messages)
}

// ModifiedBuild reports whether the plan's commits name, in Generated-By,
// a dockhand built from uncommitted source, which nobody else can find.
func (p TidyPlan) ModifiedBuild() bool {
	return slices.ContainsFunc(p.Groups, func(group TidyGroup) bool { return commitmsg.ModifiedBuild(group.Message) })
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
	var result TidyResult
	err := e.history().With(ctx, plan.Branch, func(ctx context.Context) error {
		var err error
		result, err = e.applyTidy(ctx, plan)
		return err
	})
	return result, err
}

// applyTidy makes a tidy's commits and moves the branch to them, holding
// the branch's history lock (withHistory).
func (e *Engine) applyTidy(ctx context.Context, plan TidyPlan) (TidyResult, error) {
	worktree, err := e.worktree(ctx, plan.Branch)
	if err != nil {
		return TidyResult{}, err
	}
	head, final, err := worktree.WorkingTree(ctx)
	if err != nil {
		return TidyResult{}, err
	}
	current, err := e.Branch(ctx, plan.Branch.ID)
	if err != nil {
		return TidyResult{}, err
	}
	if head != plan.Head || final != plan.Final || string(current.Base) != plan.Base {
		return TidyResult{}, fmt.Errorf("%w; run dockhand tidy again", ErrStalePlan)
	}
	index, err := worktree.IndexTree(ctx)
	if err != nil {
		return TidyResult{}, err
	}
	if plan.Index != "" && index != plan.Index {
		return TidyResult{}, fmt.Errorf("%w: something was staged since the plan was made; run dockhand tidy again", ErrStalePlan)
	}
	committer, err := worktree.Author(ctx)
	if err != nil {
		return TidyResult{}, err
	}
	committer.When = e.now()
	// The index is kept, reachable, before anything moves: it can hold
	// staged content neither the old head nor the working files have.
	keptIndex, err := worktree.WriteCommit(ctx, git.Commit{Tree: index, Parents: []string{plan.Head}, Message: "dockhand: the index before tidy\n", Author: committer, Committer: committer})
	if err != nil {
		return TidyResult{}, err
	}
	trees, err := worktree.CommitTrees(ctx, []string{plan.Base})
	if err != nil {
		return TidyResult{}, err
	}
	parent, tree := plan.Base, trees[plan.Base]
	var result TidyResult
	// A commit the branch has already, at the same place, from the same
	// parent, with the same tree, author, and message (commitmsg.Unchanged),
	// stays as it is: written again, it has another committer time, so a
	// re-submit replaced the pull request's history rather than adding a
	// commit to it (#35044, adding py-flatbuffers, finding 3). Once one is
	// made anew, the rest are, their parents being new.
	keeping := true
	for i, group := range plan.Groups {
		if tree, err = worktree.ComposeTree(ctx, tree, plan.Final, group.Paths); err != nil {
			return TidyResult{}, err
		}
		if keeping && i < len(plan.History) && unchangedCommit(plan.History[i], parent, tree, group) {
			parent = plan.History[i].ID
			result.Commits = append(result.Commits, parent)
			result.Kept++
			continue
		}
		keeping = false
		if parent, err = worktree.WriteCommit(ctx, git.Commit{Tree: tree, Parents: []string{parent}, Message: group.Message, Author: group.Author, Committer: committer}); err != nil {
			return TidyResult{}, err
		}
		result.Commits = append(result.Commits, parent)
	}
	if tree != plan.Final {
		return TidyResult{}, fmt.Errorf("engine: the tidied commits end at tree %s, not the working files' %s; nothing was changed", tree, plan.Final)
	}

	// The checkpoint is recorded before the branch moves, the refs move
	// together, and the checkpoint is settled after.
	checkpoint := model.Checkpoint{Kind: model.CheckpointTidy, Branch: plan.Branch.ID, Before: model.ObjectID(plan.Head), After: model.ObjectID(parent),
		BaseBefore: model.ObjectID(plan.Base), BaseAfter: model.ObjectID(plan.Base), Index: model.ObjectID(index), At: e.now()}
	if err := e.history().Prepare(ctx, &checkpoint); err != nil {
		return TidyResult{}, err
	}
	failpoint.Hit("tidy.prepared")
	if err := e.history().Step("prepared"); err != nil {
		return TidyResult{}, err
	}
	if err := worktree.UpdateRefs(ctx, []git.RefChange{
		{Name: checkpoint.Ref(), Desired: git.RefValue{Exists: true, Object: plan.Head}},
		{Name: checkpoint.IndexRef(), Desired: git.RefValue{Exists: true, Object: keptIndex}},
		{Name: "refs/heads/" + plan.Branch.Name, Expected: git.RefValue{Exists: true, Object: plan.Head}, Desired: git.RefValue{Exists: true, Object: parent}},
	}); err != nil {
		return TidyResult{}, errors.Join(fmt.Errorf("%w; nothing was changed: %w", ErrStalePlan, err), e.history().Settle(ctx, checkpoint, model.CheckpointAbandoned, ""))
	}
	checkpoint.State = model.CheckpointApplied
	result.Checkpoint = checkpoint
	if err := e.history().Step("moved"); err != nil {
		return result, err
	}
	if err := worktree.ResetIndex(ctx); err != nil {
		return result, fmt.Errorf("the commits are made, but resetting the index failed: %w; git reset brings it in line", err)
	}
	// A narrowing that fails leaves the worktree as wide as it was.
	if narrowed, err := e.narrow(ctx, worktree, plan.Branch, plan.Base, plan.Final); err == nil {
		result.Narrowed = narrowed
	}
	message := fmt.Sprintf("tidied %s into %s (checkpoint %s)", prose.Plural(len(plan.History), "commit"), prose.Plural(len(result.Commits), "commit"), checkpoint.Name())
	if err := e.history().Settle(ctx, checkpoint, model.CheckpointApplied, message); err != nil {
		return result, history.Unfinished("the commits are made", err)
	}
	return result, nil
}

// narrow leaves out of a worktree dockhand made the directories the branch
// doesn't change, once its work is committed: edit jq widened the ov
// branch's worktree with sysutils/jq for good (the ov run's finding 6). Its
// cone is _resources and the ports the branch changes, as start and
// checkOutAgain make it. A worktree the person made is theirs, as wide as
// they made it, and one that isn't sparse is whole; nothing's narrowed
// before the work is committed, when an edit may be under way.
func (e *Engine) narrow(ctx context.Context, worktree *git.Repository, branch model.Branch, base, final string) ([]string, error) {
	if !branch.Managed {
		return nil, nil
	}
	cone, err := worktree.SparseCone(ctx)
	if err != nil || len(cone) == 0 {
		return nil, err
	}
	trees, err := worktree.CommitTrees(ctx, []string{base})
	if err != nil {
		return nil, err
	}
	changed, err := worktree.ChangedPaths(ctx, trees[base], final)
	if err != nil {
		return nil, err
	}
	want := append([]string{macports.ResourcesDirectory}, macports.ScopeOf(changed).Ports...)
	var left []string
	for _, directory := range cone {
		if !slices.Contains(want, directory) {
			left = append(left, directory)
		}
	}
	if len(left) == 0 {
		return nil, nil
	}
	return left, worktree.NarrowSparse(ctx, want)
}

// unchangedCommit reports whether a branch's commit can stand for the one a
// group would write on parent with tree.
func unchangedCommit(had git.HistoryCommit, parent, tree string, group TidyGroup) bool {
	return len(had.Parents) == 1 && had.Parents[0] == parent && had.Tree == tree &&
		had.Author.Name == group.Author.Name && had.Author.Email == group.Author.Email && had.Author.When.Equal(group.Author.When) &&
		commitmsg.Unchanged(had.Message, group.Message)
}

// Restore puts a branch's history back as a checkpoint kept it, when the
// branch and its index are still where the tidy or rebase that made it
// left them. A tidy's puts the index back as it kept that, and leaves the
// working files alone, so edits tidy had committed read as uncommitted
// again. A rebase's puts back the files too, as they were before it, and
// the master the branch started from, in the transaction that marks it
// restored.
func (e *Engine) Restore(ctx context.Context, name string) (model.Checkpoint, model.Branch, error) {
	kind, digits, _ := strings.Cut(name, "-")
	number, err := strconv.Atoi(digits)
	if err != nil || !named(kind) {
		return model.Checkpoint{}, model.Branch{}, fmt.Errorf("%q is not a checkpoint name, such as tidy-3", name)
	}
	checkpoint, branch, err := e.checkpointNamed(ctx, name, kind, number)
	if err != nil {
		return checkpoint, branch, err
	}
	err = e.history().With(ctx, branch, func(ctx context.Context) error {
		// What a stopped dockhand left is settled now; read it again.
		if checkpoint, branch, err = e.checkpointNamed(ctx, name, kind, number); err != nil {
			return err
		}
		checkpoint, branch, err = e.restore(ctx, checkpoint, branch)
		return err
	})
	return checkpoint, branch, err
}

// LatestCheckpoint is the name of a branch's latest tidy or rebase that
// undo can take back: applied, and not restored since. Where it has none,
// the error says so.
func (e *Engine) LatestCheckpoint(ctx context.Context, branch model.Branch) (string, error) {
	var name string
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		checkpoints, err := r.Checkpoints(branch.ID)
		if err != nil {
			return err
		}
		for i := len(checkpoints) - 1; i >= 0; i-- {
			if c := checkpoints[i]; c.State == model.CheckpointApplied && c.RestoredAt == nil {
				name = c.Name()
				return nil
			}
		}
		return fmt.Errorf("%s has no tidy or rebase to undo", branch.ShortName())
	})
	return name, err
}

// checkpointNamed reads a checkpoint by its name's kind and number, and
// its branch.
func (e *Engine) checkpointNamed(ctx context.Context, name, kind string, number int) (model.Checkpoint, model.Branch, error) {
	var checkpoint model.Checkpoint
	var branch model.Branch
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		checkpoint, err = r.Checkpoint(number)
		if errors.Is(err, store.ErrNotFound) || err == nil && string(checkpoint.Kind) != kind {
			return fmt.Errorf("there is no checkpoint %s", name)
		}
		if err != nil {
			return err
		}
		branch, err = r.Branch(checkpoint.Branch)
		return err
	})
	return checkpoint, branch, err
}

// restore puts a checkpoint's history back, holding the branch's history
// lock (withHistory), and returns the checkpoint and branch as they are
// after.
func (e *Engine) restore(ctx context.Context, checkpoint model.Checkpoint, branch model.Branch) (model.Checkpoint, model.Branch, error) {
	name := checkpoint.Name()
	switch {
	case checkpoint.State == model.CheckpointAbandoned:
		return checkpoint, branch, fmt.Errorf("%s was never made: dockhand stopped before its change, so there is nothing to restore", name)
	case checkpoint.State != model.CheckpointApplied:
		return checkpoint, branch, fmt.Errorf("%s isn't finished: %s isn't checked out as a branch, so dockhand can't tell whether its change was made", name, branch.Name)
	case checkpoint.RestoredAt != nil:
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
	if checkpoint.Index != "" {
		// The rewrite left the index at its new head's tree; anything
		// else is staged work that restoring would replace.
		index, err := worktree.IndexTree(ctx)
		if err != nil {
			return checkpoint, branch, err
		}
		trees, err := worktree.CommitTrees(ctx, []string{string(checkpoint.After)})
		if err != nil {
			return checkpoint, branch, err
		}
		if index != trees[string(checkpoint.After)] {
			return checkpoint, branch, fmt.Errorf("something was staged in %s since %s; restoring the index would replace it, so nothing was changed. Commit it, or set it aside (git stash), first", branch.Name, name)
		}
	}
	if checkpoint.Kind == model.CheckpointRebase {
		// A rebase ran with nothing uncommitted and left the files as the
		// rebased commit has them. They go back with the history, or
		// master's newer files would read as the branch's own edits.
		if err := worktree.MoveCheckout(ctx, string(checkpoint.After), string(checkpoint.Before)); err != nil {
			return checkpoint, branch, fmt.Errorf("restoring %s puts back master's older files too, and a change to one of them stops it, so nothing was changed. Commit it (dockhand tidy), or set it aside, first: %w", name, err)
		}
	} else {
		if err := worktree.UpdateRefs(ctx, []git.RefChange{{Name: "refs/heads/" + branch.Name,
			Expected: git.RefValue{Exists: true, Object: string(checkpoint.After)}, Desired: git.RefValue{Exists: true, Object: string(checkpoint.Before)}}}); err != nil {
			return checkpoint, branch, err
		}
		if checkpoint.Index != "" {
			if err := worktree.SetIndex(ctx, string(checkpoint.Index)); err != nil {
				return checkpoint, branch, err
			}
		} else if err := worktree.ResetIndex(ctx); err != nil {
			return checkpoint, branch, err
		}
	}
	if err := e.history().Step("moved"); err != nil {
		return checkpoint, branch, err
	}
	restored := e.now()
	checkpoint.RestoredAt = &restored
	after, err := e.history().RecordRestore(ctx, checkpoint, fmt.Sprintf("restored %s's history from %s", branch.Name, name))
	if err != nil {
		return checkpoint, branch, history.Unfinished("the history is restored", err)
	}
	return checkpoint, after, nil
}
