package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
)

// PortDiff is how a branch changes one port directory.
type PortDiff struct {
	Directory string
	// Kind is revision-only when the source proves it, else substantive.
	Kind model.TargetKind
	// Added and Deleted say the Portfile is new, or gone.
	Added, Deleted bool
}

// BranchDiff is a branch's change from its base, as its files are now:
// what check would capture and a pull request would show.
type BranchDiff struct {
	Status   BranchStatus
	BaseTree string
	Ports    []PortDiff
	// Other lists changed paths CI builds nothing for: _resources, and
	// files in a port's directory outside its Portfile and files/.
	Other []string
	// Files lists every changed path, narrowed to the paths asked for.
	Files []string
	Patch []byte
}

// Diff compares a branch's files as they are now, uncommitted edits
// included, with its base, narrowed to paths when any are given.
func (e *Engine) Diff(ctx context.Context, branch model.Branch, paths []string) (BranchDiff, error) {
	status, err := e.BranchStatus(ctx, branch)
	if err != nil {
		return BranchDiff{}, err
	}
	diff := BranchDiff{Status: status}
	if status.Missing {
		return diff, fmt.Errorf("the Git branch %s is gone", branch.Name)
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(branch.Base)})
	if err != nil {
		return diff, err
	}
	diff.BaseTree = trees[string(branch.Base)]
	changed, err := e.Repo.ChangedPaths(ctx, diff.BaseTree, status.Tree)
	if err != nil {
		return diff, err
	}
	for _, directory := range status.Scope.Ports {
		port := PortDiff{Directory: directory}
		if port.Kind, err = e.targetKind(ctx, diff.BaseTree, status.Tree, directory, changed); err != nil {
			return diff, err
		}
		before, _, err := e.Repo.File(ctx, diff.BaseTree, directory+"/Portfile")
		if err != nil {
			return diff, err
		}
		after, _, err := e.Repo.File(ctx, status.Tree, directory+"/Portfile")
		if err != nil {
			return diff, err
		}
		port.Added, port.Deleted = !before.Exists && after.Exists, before.Exists && !after.Exists
		diff.Ports = append(diff.Ports, port)
	}
	for _, path := range changed {
		if !portChange.MatchString(path) {
			diff.Other = append(diff.Other, path)
		}
		if within(path, paths) {
			diff.Files = append(diff.Files, path)
		}
	}
	diff.Patch, err = e.Repo.DiffTrees(ctx, diff.BaseTree, status.Tree, paths...)
	return diff, err
}

// within reports whether path is one of paths, or under one; any path is
// within none.
func within(path string, paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	return slices.ContainsFunc(paths, func(prefix string) bool {
		prefix = strings.TrimSuffix(prefix, "/")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	})
}

// Dependent is a port that depends directly on a port a branch changes.
type Dependent struct {
	Name      string
	Directory string
	// On names the changed ports it depends on.
	On []string
	// Phases are how: "build", "library", or "runtime".
	Phases []string
}

// DependentReader finds the direct dependents of the ports some
// directories define. MacPorts' port index is the real one.
type DependentReader interface {
	Dependents(ctx context.Context, source model.Source, directories []string) ([]Dependent, error)
}

// SharedFile is a changed file that ports load rather than own.
type SharedFile struct {
	Path string
	// PortGroup names the group the file defines, as "name version".
	PortGroup string
	// Users are the directories whose Portfiles load it, at the base.
	Users []string
}

// Impact is what a branch's change reaches beyond the ports it changes
// (Design v3 §6.7): the changed ports, the others that depend on them,
// and the shared files it changes. Dependents are candidates to look at,
// not proof of anything.
type Impact struct {
	Diff BranchDiff
	// Of are the directories whose dependents were looked for.
	Of         []string
	Dependents []Dependent
	// Unread says why dependents could not be read, when they couldn't.
	Unread string
	Shared []SharedFile
}

var portGroupFile = regexp.MustCompile(`^_resources/port1\.0/group/(.+)-([0-9][^-/]*)\.tcl$`)

// Impact reads what a branch's change reaches. Dependents are looked for
// of the named ports, or else of every port the branch changes beyond its
// revision, from the index at the branch's base.
func (e *Engine) Impact(ctx context.Context, branch model.Branch, ports []string) (Impact, error) {
	diff, err := e.Diff(ctx, branch, nil)
	impact := Impact{Diff: diff}
	if err != nil {
		return impact, err
	}
	source := model.Source{Commit: branch.Base, Base: branch.Base, Tree: model.ObjectID(diff.BaseTree)}
	for _, name := range ports {
		directory := ""
		for _, port := range diff.Ports {
			if directoryName(port.Directory) == name {
				directory = port.Directory
			}
		}
		if directory == "" {
			reader, err := e.portReader()
			if err != nil {
				return impact, err
			}
			if directory, err = reader.Directory(ctx, source, name); err != nil {
				return impact, fmt.Errorf("%s: %w", name, err)
			}
		}
		if !slices.Contains(impact.Of, directory) {
			impact.Of = append(impact.Of, directory)
		}
	}
	if len(ports) == 0 {
		for _, port := range diff.Ports {
			if port.Kind != model.RevisionOnly && !port.Added {
				impact.Of = append(impact.Of, port.Directory)
			}
		}
	}
	if len(impact.Of) > 0 {
		dependents, err := e.dependents(ctx, source, impact.Of)
		if err != nil {
			impact.Unread = err.Error()
		}
		for _, dependent := range dependents {
			if !slices.Contains(diff.Status.Scope.Ports, dependent.Directory) {
				impact.Dependents = append(impact.Dependents, dependent)
			}
		}
	}
	for _, path := range diff.Other {
		shared := SharedFile{Path: path}
		if match := portGroupFile.FindStringSubmatch(path); match != nil {
			shared.PortGroup = match[1] + " " + match[2]
			pattern := `^[[:space:]]*PortGroup[[:space:]]+` + regexp.QuoteMeta(match[1]) + `[[:space:]]+` + regexp.QuoteMeta(match[2]) + `([[:space:]]|$)`
			portfiles, err := e.Repo.GrepTree(ctx, diff.BaseTree, pattern, "*/Portfile")
			if err != nil {
				return impact, err
			}
			for _, portfile := range portfiles {
				shared.Users = append(shared.Users, strings.TrimSuffix(portfile, "/Portfile"))
			}
		} else if !strings.HasPrefix(path, "_resources/") {
			continue
		}
		impact.Shared = append(impact.Shared, shared)
	}
	return impact, nil
}

// dependents asks the engine's DependentReader, or the port reader when
// it can answer.
func (e *Engine) dependents(ctx context.Context, source model.Source, directories []string) ([]Dependent, error) {
	reader := e.DependentReader
	if reader == nil {
		ports, err := e.portReader()
		if err != nil {
			return nil, err
		}
		var ok bool
		if reader, ok = ports.(DependentReader); !ok {
			return nil, fmt.Errorf("nothing here reads dependents")
		}
	}
	return reader.Dependents(ctx, source, directories)
}

// LinkedPorts is what update --revbump-dependents would bump for a port:
// its direct library dependents in the port index at the branch's base
// (Design v3 §6.7), one per directory, less the directories of the ports
// excepted and those the branch already changes.
type LinkedPorts struct {
	Base model.ObjectID
	// Bump are the dependents to revision-bump, in name order.
	Bump []Dependent
	// Changed are dependents the branch already changes, left alone.
	Changed []Dependent
	// Excepted are the dependents --except took out.
	Excepted []string
}

// LinkedPorts reads a port's direct library dependents for a branch.
func (e *Engine) LinkedPorts(ctx context.Context, branch model.Branch, port string, except []string) (LinkedPorts, error) {
	linked := LinkedPorts{Base: branch.Base}
	status, err := e.BranchStatus(ctx, branch)
	if err != nil {
		return linked, err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(branch.Base)})
	if err != nil {
		return linked, err
	}
	directory, err := e.findDirectory(ctx, trees[string(branch.Base)], port)
	if err != nil {
		return linked, err
	}
	source := model.Source{Commit: branch.Base, Base: branch.Base, Tree: model.ObjectID(trees[string(branch.Base)])}
	dependents, err := e.dependents(ctx, source, []string{directory})
	if err != nil {
		return linked, fmt.Errorf("reading %s's dependents: %w", port, err)
	}
	for _, name := range except {
		if !slices.ContainsFunc(dependents, func(d Dependent) bool { return d.Name == name && slices.Contains(d.Phases, "library") }) {
			return linked, fmt.Errorf("--except %s: it is not a library dependent of %s", name, port)
		}
	}
	// Leaving a port out leaves its directory alone, subports and all, since
	// they may share its revision.
	excepted := map[string]bool{}
	for _, dependent := range dependents {
		if slices.Contains(except, dependent.Name) {
			excepted[dependent.Directory] = true
		}
	}
	seen := map[string]bool{}
	for _, dependent := range dependents {
		switch {
		case !slices.Contains(dependent.Phases, "library") || seen[dependent.Directory]:
		case excepted[dependent.Directory]:
			if slices.Contains(except, dependent.Name) {
				linked.Excepted = append(linked.Excepted, dependent.Name)
			}
		case slices.Contains(status.Scope.Ports, dependent.Directory):
			linked.Changed = append(linked.Changed, dependent)
		default:
			seen[dependent.Directory] = true
			linked.Bump = append(linked.Bump, dependent)
		}
	}
	return linked, nil
}

// LinkedRevbump is what update --revbump-dependents did for an updated
// port, or with a plan would do.
type LinkedRevbump struct {
	LinkedPorts
	// Subject is the commit subject recorded for tidy, after each port's
	// name: "rebuild for <port> <version>".
	Subject string
	// Bumped are the dependents whose revision was bumped, in order; none
	// for a plan.
	Bumped []string
}

// RevbumpLinked bumps the revision of the ports that link an updated one
// directly (LinkedPorts), or with plan only finds them. It stops at the
// first port it can't bump, naming it; the ports before it stay bumped,
// as edits in the branch's files.
func (e *Engine) RevbumpLinked(ctx context.Context, branch model.Branch, update Update, except []string, plan bool) (LinkedRevbump, error) {
	linked, err := e.LinkedPorts(ctx, branch, update.Port, except)
	result := LinkedRevbump{LinkedPorts: linked, Subject: fmt.Sprintf("rebuild for %s %s", update.Port, update.After.Version)}
	if err != nil || plan {
		return result, err
	}
	for _, dependent := range linked.Bump {
		if _, err := e.Update(ctx, UpdateRequest{Branch: branch, Action: record.BumpRevision, Port: dependent.Name, Subject: result.Subject}); err != nil {
			return result, fmt.Errorf("revision-bumping %s: %w; the ports before it are bumped", dependent.Name, err)
		}
		result.Bumped = append(result.Bumped, dependent.Name)
	}
	return result, nil
}
