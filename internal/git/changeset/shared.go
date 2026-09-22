package changeset

import (
	"context"
	"path"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

const groupDirectory = Resources + "/port1.0/group/"

// SharedUsers lists the files under _resources a contribution changes and,
// for each PortGroup among them, the Portfiles and other group files that
// load it at the contribution's tree, found by their PortGroup line. A
// contribution with no shared change lists nothing.
func SharedUsers(ctx context.Context, repo *git.Repository, source record.Source) ([]record.SharedFile, error) {
	if source.Base == "" {
		return nil, nil
	}
	delta, err := Between(ctx, repo, source.Base, source.Tree)
	if err != nil {
		return nil, err
	}
	var shared []record.SharedFile
	for _, name := range delta.Paths {
		if !strings.HasPrefix(name, Resources+"/") {
			continue
		}
		file := record.SharedFile{Path: name}
		if group, version, ok := groupOf(name); ok {
			pattern := "^[[:space:]]*PortGroup[[:space:]]+" + regexp.QuoteMeta(group) + "[[:space:]]+" + regexp.QuoteMeta(version) + "([[:space:]]|$)"
			file.Loaders, err = repo.GrepTree(ctx, string(source.Tree), pattern, "*/*/Portfile", groupDirectory+"*.tcl")
			if err != nil {
				return nil, err
			}
		}
		shared = append(shared, file)
	}
	return shared, nil
}

// groupOf reads a PortGroup's name and version from its file path,
// _resources/port1.0/group/<name>-<version>.tcl.
func groupOf(name string) (group, version string, ok bool) {
	if !strings.HasPrefix(name, groupDirectory) || path.Dir(name) != strings.TrimSuffix(groupDirectory, "/") {
		return "", "", false
	}
	base, found := strings.CutSuffix(path.Base(name), ".tcl")
	if !found {
		return "", "", false
	}
	i := strings.LastIndex(base, "-")
	if i <= 0 || i == len(base)-1 {
		return "", "", false
	}
	return base[:i], base[i+1:], true
}
