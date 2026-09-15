package app

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/record"
	githubverify "github.com/herbygillot/dockhand/internal/verify/github"
	"strings"
)

func (s *Services) githubBuild(ctx context.Context, platform record.Platform, needsXcode bool) (record.BuildConfig, error) {
	user, err := s.githubClient.AuthenticatedUser(ctx)
	if err != nil {
		return record.BuildConfig{}, err
	}
	destination, err := s.Workflow.Publisher.Destination(ctx, s.githubDestination)
	if err != nil {
		return record.BuildConfig{}, err
	}
	head, err := s.githubClient.RepositoryInfo(ctx, destination.HeadRepository)
	if err != nil {
		return record.BuildConfig{}, err
	}
	owner, _, _ := strings.Cut(head.Name, "/")
	if !strings.EqualFold(owner, user) || !strings.EqualFold(head.Parent, "macports/macports-ports") {
		return record.BuildConfig{}, fmt.Errorf("github verification requires your personal fork of macports/macports-ports; select it with --remote")
	}
	api, err := s.githubClient.Actions(ctx, destination.HeadRepository)
	if err != nil {
		return record.BuildConfig{}, err
	}
	flow, err := api.Workflow(ctx, "main.yml")
	if err != nil {
		return record.BuildConfig{}, fmt.Errorf("github verification: enable main.yml in your fork's Actions settings: %w", err)
	}
	if flow.GetState() != "active" || flow.GetPath() != githubverify.WorkflowPath {
		return record.BuildConfig{}, fmt.Errorf("github verification: enable main.yml in your fork's Actions settings")
	}
	return githubverify.BuildConfig(platform, githubverify.Config{Destination: destination, WorkflowID: flow.GetID()}, needsXcode)
}
