package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// IndexFailureReader says why a source's port index left out the
// Portfiles of some directories, portindex's reason for each it couldn't
// parse, by directory; the port reader that reads the index is one.
type IndexFailureReader interface {
	IndexFailures(ctx context.Context, source model.Source, directories []string) (map[string]string, error)
}

// unindexedPort is a name the branch's port index doesn't hold, said by
// what became of the directory master has the port in: words, one of
// brokenPortfileWords, unexplainedPortfileWords, renamedPortWords, or
// removedDirectoryWords. It keeps the lookup's refusal, and so
// selection.ErrUnknownPort, in its chain.
type unindexedPort struct {
	words string
	err   error
}

func (u unindexedPort) Error() string { return u.words }
func (u unindexedPort) Unwrap() error { return u.err }

// brokenPortfileWords are (a): nothing indexed at the directory, and
// portindex's reason kept.
func brokenPortfileWords(portfile, name, branch, reason string) string {
	return fmt.Sprintf("%s doesn't parse, so no port named %s is in %s's index: portindex says %s. Fix the Portfile and run the command again", portfile, name, branch, reason)
}

// unexplainedPortfileWords are (a'): nothing indexed at the directory,
// which the branch changes, and no reason kept, as for an index cached
// before reasons were.
func unexplainedPortfileWords(portfile, directory, branch string) string {
	return fmt.Sprintf("%s, which %s changes, isn't in its port index, and portindex didn't say why; port -D %s info shows what Tcl refuses", portfile, branch, directory)
}

// renamedPortWords are (b): the directory's ports indexed under other
// names, now its names in the branch's index; asked, the one name new to
// the directory, where one is, else empty.
func renamedPortWords(portfile, name string, now []string, asked string) string {
	words := fmt.Sprintf("%s names its port %s now, where master names it %s.", portfile, now[0], name)
	if len(now) > 1 {
		words = fmt.Sprintf("%s names its ports %s now, where master has %s among them.", portfile, prose.And(now), name)
	}
	if asked == "" {
		return words + " Restore the name line, or ask for one of them"
	}
	return fmt.Sprintf("%s Restore the name line, or ask for %s; a port renamed on purpose goes through dockhand rename %s %s", words, asked, name, asked)
}

// removedDirectoryWords are (c): the directory gone from the branch.
func removedDirectoryWords(directory, name, branch string) string {
	return fmt.Sprintf("%s, where master has %s, isn't in %s, and its index has no port there; dockhand move %s <category> is how a port moves, and dockhand status -p %s says what the branch changes", directory, name, branch, name, name)
}

// unindexed says why a port name the branch's index lacks isn't there,
// where the lookup refused it as selection.ErrUnknownPort and the
// directory master has the port in says: the directory as the base's
// index names it, else category/<name>, where the base's index lacks it
// too; then the branch's index at that directory. branch is the branch's
// short name; starting is work on master for a branch to be started for
// the port, whose index is master's, and branch then "master". A name
// master doesn't have, one whose directory the branch leaves as master
// has it, outside a start, or what can't be read keeps today's words.
func (e *Engine) unindexed(ctx context.Context, err error, source model.Source, branch, name string, starting bool) error {
	if !errors.Is(err, selection.ErrUnknownPort) || source.Base == "" {
		return err
	}
	ports, capErr := e.capabilities()
	if capErr != nil || ports.Reader == nil || ports.Names == nil {
		return err
	}
	trees, treeErr := e.Repo.CommitTrees(ctx, []string{string(source.Base)})
	baseTree := trees[string(source.Base)]
	if treeErr != nil || baseTree == "" {
		return err
	}
	base := model.Source{Commit: source.Base, Tree: model.ObjectID(baseTree)}
	directory, indexed := "", false
	if baseTree != string(source.Tree) {
		if found, findErr := ports.Reader.Directory(ctx, base, name); findErr == nil {
			directory, indexed = found, true
		}
	}
	if directory == "" {
		named, namedErr := e.directoriesNamed(ctx, baseTree, name)
		if namedErr != nil || len(named) != 1 {
			return err
		}
		directory = named[0]
	}
	paths, diffErr := e.Repo.ChangedPaths(ctx, baseTree, string(source.Tree))
	if diffErr != nil {
		return err
	}
	changes := slices.Contains(macports.ScopeOf(paths).Ports, directory)
	if !changes && (indexed || !starting) {
		return err
	}
	portfile := directory + "/Portfile"
	if found, blobErr := e.Repo.FileBlobs(ctx, string(source.Tree), []string{portfile}); blobErr != nil {
		return err
	} else if _, ok := found[portfile]; !ok {
		if !changes {
			return err
		}
		return unindexedPort{words: removedDirectoryWords(directory, name, branch), err: err}
	}
	defined, namesErr := ports.Names.PortsDefined(ctx, source, []string{directory})
	if namesErr != nil {
		return err
	}
	if now := defined[directory]; len(now) > 0 {
		// A Portfile master names otherwise than its directory is no
		// rename of the branch's.
		if !changes {
			return err
		}
		var before []string
		if indexed {
			master, _ := ports.Names.PortsDefined(ctx, base, []string{directory})
			before = master[directory]
		}
		now = slices.Clone(now)
		slices.SortFunc(now, macports.ComparePortNames)
		var added []string
		for _, port := range now {
			if !slices.Contains(before, port) {
				added = append(added, port)
			}
		}
		asked := ""
		if len(added) == 1 {
			asked = added[0]
		}
		return unindexedPort{words: renamedPortWords(portfile, name, now, asked), err: err}
	}
	if ports.Failures != nil {
		failed, failErr := ports.Failures.IndexFailures(ctx, source, []string{directory})
		if reason, ok := failed[directory]; failErr == nil && ok {
			return unindexedPort{words: brokenPortfileWords(portfile, name, branch, reason), err: err}
		}
	}
	if !changes {
		return err
	}
	return unindexedPort{words: unexplainedPortfileWords(portfile, directory, branch), err: err}
}
