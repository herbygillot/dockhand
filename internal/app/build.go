package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/verify"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/workflow"
)

type tartBuildConfigurator interface {
	BuildConfig(context.Context, record.Platform, tart.BuildOptions) (record.BuildConfig, error)
}

func (s *Services) buildResolver(platform record.Platform, tests record.TestPolicy, fromSource, preserve bool) workflow.BuildResolver {
	return func(ctx context.Context, evaluation macports.Snapshot) (workflow.BuildResolution, error) {
		if err := ctx.Err(); err != nil {
			return workflow.BuildResolution{}, err
		}
		needsXcode, err := evaluation.RequiresXcode()
		if err != nil {
			return workflow.BuildResolution{}, err
		}
		githubBuild := func() (workflow.BuildResolution, error) {
			policy := tests
			if policy == "" {
				policy = record.TestWorkflow
			}
			if policy != record.TestWorkflow || fromSource || len(evaluation.Target.Variants) != 0 {
				return workflow.BuildResolution{}, fmt.Errorf("github verification uses --tests workflow and default variants and dependency policy; select --provider tart for local policies")
			}
			config, err := s.githubBuild(ctx, platform, needsXcode)
			if err != nil {
				return workflow.BuildResolution{}, err
			}
			progress.VerboseReport(ctx, "Verification provider: github (pushes the candidate to your fork)")
			return workflow.BuildResolution{Build: &config}, nil
		}
		if s.providerName == verify.ProviderGitHub {
			return githubBuild()
		}
		policy := tests
		if policy == "" {
			policy = record.TestDeclared
		}
		config, err := s.tartVerification.BuildConfig(ctx, platform, tart.BuildOptions{Tests: policy, FromSource: fromSource, NeedsXcode: needsXcode})
		if err == nil {
			targets := map[string]record.BuildConfig{}
			if len(s.targetImages) > 0 {
				binder, ok := s.tartVerification.(interface {
					BuildConfigForImage(context.Context, record.Platform, tart.BuildOptions, string) (record.BuildConfig, error)
				})
				if !ok {
					return workflow.BuildResolution{}, fmt.Errorf("Tart image selection is unavailable")
				}
				for _, name := range slices.Sorted(maps.Keys(s.targetImages)) {
					if name == evaluation.Target.Name {
						return workflow.BuildResolution{}, fmt.Errorf("use --image for root target %s", name)
					}
					bound, err := binder.BuildConfigForImage(ctx, platform, tart.BuildOptions{Tests: policy, FromSource: fromSource, NeedsXcode: needsXcode}, s.targetImages[name])
					if err != nil {
						return workflow.BuildResolution{}, fmt.Errorf("image for %s: %w", name, err)
					}
					targets[name] = bound
				}
			}
			progress.VerboseReport(ctx, "Verification provider: tart; no GitHub verification will be submitted")
			return workflow.BuildResolution{Build: &config, TargetBuilds: targets}, nil
		}
		if ctx.Err() != nil {
			return workflow.BuildResolution{}, ctx.Err()
		}
		if s.providerName == "auto" {
			if errors.Is(err, tart.ErrImageUnavailable) {
				setup := "dockhand setup"
				if needsXcode {
					setup = "dockhand setup --xcode <archive-or-directory>"
				}
				progress.Report(ctx, "No suitable prepared Tart image is available. Run %s to verify locally. Using GitHub verification.", setup)
			} else if errors.Is(err, tart.ErrExecutableUnavailable) {
				progress.Report(ctx, "Tart is not available; using GitHub verification.")
			} else {
				return workflow.BuildResolution{}, err
			}
			resolved, githubErr := githubBuild()
			if githubErr != nil && preserve && ctx.Err() == nil {
				return workflow.BuildResolution{Problem: "GitHub verification could not be configured: " + githubErr.Error()}, nil
			}
			return resolved, githubErr
		}
		if len(s.targetImages) > 0 {
			return workflow.BuildResolution{}, err
		}
		if preserve {
			requirements := &record.BuildRequirements{Provider: verify.ProviderTart, Platform: platform, NeedsXcode: needsXcode, CapabilitiesRequired: true, Tests: policy, FromSource: fromSource}
			return workflow.BuildResolution{Requirements: requirements, Problem: err.Error()}, nil
		}
		return workflow.BuildResolution{}, err
	}
}
