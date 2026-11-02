package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// indexedAs stands in for each tree's port index as portindex builds it:
// a Portfile whose version line is misspelled "versio" fails to parse,
// with portindex's reason, and is left out; any other is indexed under its
// name line. keepsFailures false is an index cached before failures were
// kept.
type indexedAs struct {
	t             *testing.T
	root          string
	keepsFailures bool
}

var nameLine = regexp.MustCompile(`(?m)^name (\S+)$`)

// index is a tree's entries, name to directory, and its failures.
func (x indexedAs) index(tree model.ObjectID) (map[string]string, map[string]string) {
	entries, failed := map[string]string{}, map[string]string{}
	for _, file := range strings.Fields(testsupport.Git(x.t, x.root, "ls-tree", "-r", "--name-only", string(tree))) {
		if !strings.HasSuffix(file, "/Portfile") || strings.HasPrefix(file, "_resources/") {
			continue
		}
		directory := strings.TrimSuffix(file, "/Portfile")
		text := testsupport.Git(x.t, x.root, "show", string(tree)+":"+file)
		if strings.Contains(text, "\nversio ") {
			failed[directory] = `invalid command name "versio"`
			continue
		}
		if m := nameLine.FindStringSubmatch(text); m != nil {
			entries[m[1]] = directory
		}
	}
	return entries, failed
}

func (x indexedAs) Ports(context.Context, model.Source, string, model.Environment, map[string]bool) ([]macports.PortInfo, error) {
	return nil, errors.New("not evaluated here")
}

func (x indexedAs) Directory(_ context.Context, source model.Source, name string) (string, error) {
	entries, _ := x.index(source.Tree)
	if directory, ok := entries[name]; ok {
		return directory, nil
	}
	return "", treequery.NotInTree(name)
}

func (x indexedAs) PortsDefined(_ context.Context, source model.Source, directories []string) (map[string][]string, error) {
	entries, _ := x.index(source.Tree)
	defined := map[string][]string{}
	for name, directory := range entries {
		for _, wanted := range directories {
			if directory == wanted {
				defined[directory] = append(defined[directory], name)
			}
		}
	}
	return defined, nil
}

func (x indexedAs) IndexFailures(_ context.Context, source model.Source, directories []string) (map[string]string, error) {
	failed := map[string]string{}
	if !x.keepsFailures {
		return failed, nil
	}
	_, all := x.index(source.Tree)
	for _, directory := range directories {
		if reason, ok := all[directory]; ok {
			failed[directory] = reason
		}
	}
	return failed, nil
}

// lookingUp refuses a name the tree's index lacks as selection does, with
// selection.ErrUnknownPort, before preparing the edit.
type lookingUp struct {
	*fakePreparer
	index indexedAs
}

func (l lookingUp) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	if entries, _ := l.index.index(r.Source.Tree); entries[r.Selection.Selector] == "" {
		return editprep.Result{}, fmt.Errorf("no port named %s: %w", r.Selection.Selector, selection.ErrUnknownPort)
	}
	return l.fakePreparer.Prepare(ctx, r)
}

// A port name the branch's index lacks is said by what became of the
// directory master has it in: a Portfile that doesn't parse with
// portindex's reason (a), or without one where the index kept none (a'),
// a port renamed (b), the directory gone (c); a name master hasn't, or
// whose directory the branch leaves alone, still "no port named …"
// (herbygillot/dockhand#8).
func TestAPortTheBranchsEditBrokeIsSaidAsBroken(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name          string
		port          string
		keepsFailures bool
		// brokenAtMaster has master's jq Portfile fail to parse.
		brokenAtMaster bool
		edit           func(t *testing.T, worktree string)
		said           string
	}{
		{name: "broken", port: "jq", keepsFailures: true,
			edit: func(t *testing.T, worktree string) {
				testsupport.WriteFiles(t, worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversio 1.8.0\n"})
			},
			said: `textproc/jq/Portfile doesn't parse, so no port named jq is in jq-update's index: portindex says invalid command name "versio". Fix the Portfile and run the command again`},
		{name: "cached before failures were kept", port: "jq",
			edit: func(t *testing.T, worktree string) {
				testsupport.WriteFiles(t, worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversio 1.8.0\n"})
			},
			said: `textproc/jq/Portfile, which jq-update changes, isn't in its port index, and portindex didn't say why; port -D textproc/jq info shows what Tcl refuses`},
		{name: "renamed", port: "jq", keepsFailures: true,
			edit: func(t *testing.T, worktree string) {
				testsupport.WriteFiles(t, worktree, map[string]string{"textproc/jq/Portfile": "name jqq\nversion 1.8.0\n"})
			},
			said: `textproc/jq/Portfile names its port jqq now, where master names it jq. Restore the name line, or ask for jqq; a port renamed on purpose goes through dockhand rename jq jqq`},
		{name: "removed", port: "jq", keepsFailures: true,
			edit: func(t *testing.T, worktree string) {
				require.NoError(t, os.RemoveAll(filepath.Join(worktree, "textproc/jq")))
			},
			said: `textproc/jq, where master has jq, isn't in jq-update, and its index has no port there; dockhand move jq <category> is how a port moves, and dockhand status -p jq says what the branch changes`},
		{name: "a name master hasn't", port: "jqx", keepsFailures: true,
			edit: func(t *testing.T, worktree string) {
				testsupport.WriteFiles(t, worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversio 1.8.0\n"})
			},
			said: "no port named jqx: no port of that name"},
		{name: "a directory the branch leaves alone", port: "jq", keepsFailures: true, brokenAtMaster: true,
			edit: func(t *testing.T, worktree string) {
				testsupport.WriteFiles(t, worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
			},
			said: "no port named jq: no port of that name"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			if c.brokenAtMaster {
				breakAtMaster(t, f)
			}
			e, p := f.withPreparer(t)
			index := indexedAs{t: t, root: f.clone, keepsFailures: c.keepsFailures}
			usePorts(e, index)
			e.Preparer = lookingUp{fakePreparer: p, index: index}
			branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update", Here: true})
			require.NoError(t, err)
			c.edit(t, branch.Worktree)
			_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditChecksums, Port: c.port})
			require.EqualError(t, err, c.said)
			require.ErrorIs(t, err, selection.ErrUnknownPort, "create's name check reads it as before")
		})
	}
}

// breakAtMaster has upstream's jq Portfile fail to parse, as KeePassXC's
// may have on the Mac #8 came from before any edit.
func breakAtMaster(t *testing.T, f fixture) {
	t.Helper()
	testsupport.WriteFiles(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversio 1.7.1\n"})
	testsupport.Git(t, f.upstream, "commit", "-q", "-am", "jq: break it")
}

// An update starting a branch for a port master's index lacks, as its
// Portfile doesn't parse on this Mac, says so before the branch is
// started, its index master's.
func TestAPortMastersIndexLacksIsSaidAsBrokenAtTheStart(t *testing.T) {
	t.Parallel()
	f := setup(t)
	breakAtMaster(t, f)
	e, p := f.withPreparer(t)
	index := indexedAs{t: t, root: f.clone, keepsFailures: true}
	usePorts(e, index)
	e.Preparer = lookingUp{fakePreparer: p, index: index}
	_, err := e.Update(t.Context(), UpdateRequest{Start: &StartRequest{Name: "jq-update"}, Action: model.EditUpdate, Port: "jq"})
	require.EqualError(t, err, `textproc/jq/Portfile doesn't parse, so no port named jq is in master's index: portindex says invalid command name "versio". Fix the Portfile and run the command again`)
	require.ErrorIs(t, err, selection.ErrUnknownPort)
	require.Empty(t, testsupport.Git(t, f.clone, "branch", "--list", "dockhand/jq-update"), "no branch is started for an edit that couldn't be prepared")
}

// A directory whose ports the edit renamed lists them all, and asks for
// the one name new to it where one is.
func TestARenameOfSeveralPortsListsThem(t *testing.T) {
	t.Parallel()
	require.Equal(t, "security/KeePassXC/Portfile names its ports keepasxc and keepasxc-devel now, where master has keepassxc among them. Restore the name line, or ask for one of them",
		renamedPortWords("security/KeePassXC/Portfile", "keepassxc", []string{"keepasxc", "keepasxc-devel"}, ""))
	require.Equal(t, "security/KeePassXC/Portfile names its ports keepassxc-devel and keepasxc now, where master has keepassxc among them. Restore the name line, or ask for keepasxc; a port renamed on purpose goes through dockhand rename keepassxc keepasxc",
		renamedPortWords("security/KeePassXC/Portfile", "keepassxc", []string{"keepassxc-devel", "keepasxc"}, "keepasxc"))
}
