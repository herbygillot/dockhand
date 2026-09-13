package app

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

type PreviewRequest struct {
	Action    record.Action
	Branch    string
	Selection macports.Selection
	Version   string
	Reason    string
}

type Preview struct {
	Branch      string
	Preparation prepare.Result
	Diff        string
}

func PreviewPreparation(ctx context.Context, config Config, request PreviewRequest) (Preview, error) {
	if request.Action != record.BumpRevision {
		return Preview{}, fmt.Errorf("%w: %s", prepare.ErrNotImplemented, request.Action)
	}
	root := config.Repository
	if root == "" {
		root = "."
	}
	repo, err := git.Open(ctx, root, config.GitExecutable)
	if err != nil {
		return Preview{}, err
	}
	branch := request.Branch
	if branch == "" {
		branch, err = repo.CurrentBranch(ctx)
		if err != nil {
			return Preview{}, err
		}
	}
	commit, tree, err := repo.Branch(ctx, branch)
	if err != nil {
		return Preview{}, err
	}
	ports := &macports.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	service := prepare.Service{Repo: repo, Ports: ports}
	result, err := service.Prepare(ctx, prepare.Request{
		Action: request.Action, Source: record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree)},
		Selection: request.Selection, Version: request.Version, Reason: request.Reason,
	})
	if err != nil {
		return Preview{Branch: branch, Preparation: result}, err
	}
	diff, err := repo.DiffTrees(ctx, tree, string(result.PreparedTree))
	if err != nil {
		return Preview{}, err
	}
	return Preview{Branch: branch, Preparation: result, Diff: string(diff)}, nil
}
