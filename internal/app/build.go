package app

import (
	"context"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/choice"
)

// remoteBuild is the GitHub side of the provider choice: it configures a
// workflow build through the person's fork, which needs the GitHub client,
// the publisher's destination, and the forge, all of which the services
// hold. The policy that decides when to use it is workflow/choice.
type remoteBuild struct{ services *Services }

func (r remoteBuild) BuildConfig(ctx context.Context, platform record.Platform, needsXcode bool) (record.BuildConfig, error) {
	return r.services.githubBuild(ctx, platform, needsXcode)
}

// resolver is the provider choice for one verification's options.
func (s *Services) resolver(platform record.Platform, tests record.TestPolicy, fromSource, preserve bool) workflow.BuildResolver {
	return s.providers.Resolver(platform, choice.Options{Tests: tests, FromSource: fromSource, Preserve: preserve})
}
