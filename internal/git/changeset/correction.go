package changeset

import (
	"context"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// Correct constructs one contribution commit, optionally transplanting its delta
// onto a new base, without moving a branch or modifying the user's checkout.
func Correct(ctx context.Context, repo *git.Repository, snapshot Snapshot, oldBase, newBase record.ObjectID, message string, signature git.Signature) (record.Source, error) {
	commit, err := repo.WriteCommit(ctx, git.Commit{Tree: string(snapshot.Tree), Parents: []string{string(oldBase)}, Message: message, Author: signature, Committer: signature})
	if err != nil {
		return record.Source{}, err
	}
	tree := string(snapshot.Tree)
	if newBase != oldBase {
		tree, err = repo.Transplant(ctx, commit, string(newBase))
		if err != nil {
			return record.Source{}, err
		}
		commit, err = repo.WriteCommit(ctx, git.Commit{Tree: tree, Parents: []string{string(newBase)}, Message: message, Author: signature, Committer: signature})
		if err != nil {
			return record.Source{}, err
		}
	}
	return record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: newBase}, nil
}
