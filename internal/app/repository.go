package app

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
)

// openPortsTree opens the selected checkout and refuses anything that is not
// a MacPorts ports tree, before any state is registered for it or any fetch
// lands in its object store. The working tree is checked first; a sparse or
// unpopulated checkout still counts when its branch holds a
// <category>/<port>/Portfile.
func openPortsTree(ctx context.Context, root, gitExecutable string) (*git.Repository, error) {
	if root == "" {
		root = "."
	}
	repo, err := git.Open(ctx, root, gitExecutable)
	if err != nil {
		return nil, err
	}
	treeErr := macports.ValidatePortsTree(repo.Root, root)
	if treeErr == nil || branchHoldsPorts(ctx, repo) {
		return repo, nil
	}
	return nil, treeErr
}

// branchHoldsPorts reports whether a local branch's tree has a Portfile two
// levels down, the shape of a ports tree, without reading the working tree.
// The checked-out branch is asked first; a contribution branch in an
// otherwise unpopulated checkout also counts.
func branchHoldsPorts(ctx context.Context, repo *git.Repository) bool {
	var names []string
	if current, err := repo.CurrentBranch(ctx); err == nil && current != "" {
		names = append(names, current)
	}
	if refs, err := repo.ReadRefs(ctx, "refs/heads/"); err == nil {
		for name := range refs {
			names = append(names, strings.TrimPrefix(name, "refs/heads/"))
		}
	}
	for _, name := range names {
		if _, tree, err := repo.Branch(ctx, name); err == nil && treeHoldsPorts(ctx, repo, tree) {
			return true
		}
	}
	return false
}

func treeHoldsPorts(ctx context.Context, repo *git.Repository, tree string) bool {
	categories, err := repo.ReadTree(ctx, tree)
	if err != nil {
		return false
	}
	for _, category := range categories {
		if category.Type != "tree" || strings.HasPrefix(category.Name, ".") || strings.HasPrefix(category.Name, "_") {
			continue
		}
		ports, err := repo.ReadTree(ctx, category.Object)
		if err != nil {
			continue
		}
		for _, port := range ports {
			if port.Type != "tree" || strings.HasPrefix(port.Name, ".") {
				continue
			}
			files, err := repo.ReadTree(ctx, port.Object)
			if err != nil {
				continue
			}
			for _, file := range files {
				if file.Name == "Portfile" && file.Type == "blob" {
					return true
				}
			}
		}
	}
	return false
}
