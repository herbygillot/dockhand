package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
	githubverify "github.com/herbygillot/dockhand/internal/verify/github"
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
	head, err := s.Workflow.Publisher.Forge.RepositoryInfo(ctx, destination.HeadRepository)
	if err != nil {
		return record.BuildConfig{}, err
	}
	owner, _, _ := strings.Cut(head.Name, "/")
	if !strings.EqualFold(owner, user) || !strings.EqualFold(head.Parent, "macports/macports-ports") {
		return record.BuildConfig{}, fmt.Errorf("github verification requires your personal fork of macports/macports-ports; select it with --remote")
	}
	return githubverify.Configure(ctx, s.githubClient, platform, destination, needsXcode)
}
