package app

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/workflow"
)

func (s *Services) BindCorrection(ctx context.Context, request workflow.CorrectionRequest, tests record.TestPolicy, fromSource bool) (workflow.BoundCorrection, error) {
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundCorrection{}, err
	}
	request.Platform = platform
	if request.Action == record.Rebase {
		commit, _, err := s.Workflow.Repo.FetchBranch(ctx, macports.PortsRepositoryURL, macports.PortsBranch)
		if err != nil {
			return workflow.BoundCorrection{}, err
		}
		request.Base = record.ObjectID(commit)
	}
	if !request.Preview {
		request.ResolveBuild = s.buildResolver(platform, tests, fromSource, true)
	}
	return s.Workflow.BindCorrection(ctx, request)
}
func (s *Services) Reassociate(ctx context.Context, id record.ChangeID, branch string) (record.Change, error) {
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return record.Change{}, err
	}
	return s.Workflow.Reassociate(ctx, id, branch, platform)
}

// PreviewCorrection reads existing contribution state without initializing it.
func PreviewCorrection(ctx context.Context, config Config, request workflow.CorrectionRequest) (_ workflow.BoundCorrection, err error) {
	repo, err := openPortsTree(ctx, config.Repository, config.GitExecutable)
	if err != nil {
		return workflow.BoundCorrection{}, err
	}
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{ReadOnly: true})
	if err != nil {
		return workflow.BoundCorrection{}, err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	repository, err := store.FindRepository(ctx, repo.CommonDir)
	if err != nil {
		return workflow.BoundCorrection{}, err
	}
	ports := portReader(config, repo)
	services := Services{Workflow: &workflow.Engine{State: store, Repository: repository.ID, Repo: repo, Ports: ports}, ports: ports}
	request.Preview = true
	return services.BindCorrection(ctx, request, "", false)
}
