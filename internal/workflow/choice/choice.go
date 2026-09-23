// Package choice decides which verification provider builds a candidate,
// and with what configuration: the provider a person named, or under auto
// the local Tart image that serves the platform and GitHub's workflow when
// none does. It is the one place the fallback, the test policy defaults,
// the per-target image binding, and the choice between failing and
// recording a requirement are written; app wires the two providers in and
// the engine's bindings call what it returns. It knows the providers'
// configuration types and their sentinel errors, which is why it is a leaf
// beside the engine rather than part of it.
package choice

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
)

// Local configures a build in a prepared Tart image for the platform. It
// fails with verify.ErrImageUnavailable when no image serves it and with
// verify.ErrExecutableUnavailable when Tart itself is absent, which under
// auto are the two reasons to try GitHub instead.
type Local interface {
	BuildConfig(context.Context, record.Platform, verify.BuildOptions) (record.BuildConfig, error)
}

// LocalImages binds a build to a named image, for a dependent whose
// image a person chose.
type LocalImages interface {
	BuildConfigForImage(context.Context, record.Platform, verify.BuildOptions, string) (record.BuildConfig, error)
}

// Remote configures a build on GitHub's workflow, pushing the candidate to
// the person's fork.
type Remote interface {
	BuildConfig(ctx context.Context, platform record.Platform, needsXcode bool) (record.BuildConfig, error)
}

// Providers is what a choice is made between.
type Providers struct {
	// Name is the provider a person asked for: tart, github, or auto.
	Name   string
	Local  Local
	Remote Remote
	// TargetImages binds named dependents to named images; it selects Tart.
	TargetImages map[string]string
}

// Options are one verification's choices.
type Options struct {
	Tests      record.TestPolicy
	FromSource bool
	// Preserve records a local provider's failure as an evidence
	// requirement on the job, so a prepared branch survives, rather than
	// failing the binding; a preparation preserves, a verification fails.
	Preserve bool
}

// Resolver chooses, for one evaluation, the build the providers can give
// it under the options.
func (p Providers) Resolver(platform record.Platform, options Options) workflow.BuildResolver {
	return func(ctx context.Context, evaluation macports.Snapshot) (workflow.BuildResolution, error) {
		if err := ctx.Err(); err != nil {
			return workflow.BuildResolution{}, err
		}
		needsXcode, err := evaluation.RequiresXcode()
		if err != nil {
			return workflow.BuildResolution{}, err
		}
		remote := func() (workflow.BuildResolution, error) {
			policy := options.Tests
			if policy == "" {
				policy = record.TestWorkflow
			}
			if policy != record.TestWorkflow || options.FromSource || len(evaluation.Target.Variants) != 0 {
				return workflow.BuildResolution{}, fmt.Errorf("GitHub verification uses its workflow's test policy, default variants, and dependency policy; select --provider tart for local policies")
			}
			config, err := p.Remote.BuildConfig(ctx, platform, needsXcode)
			if err != nil {
				return workflow.BuildResolution{}, err
			}
			progress.VerboseReport(ctx, "Verification provider: github (pushes the candidate to your fork)")
			return workflow.BuildResolution{Build: &config}, nil
		}
		if p.Name == verify.ProviderGitHub {
			return remote()
		}
		policy := options.Tests
		if policy == "" {
			policy = record.TestDeclared
		}
		local := verify.BuildOptions{Tests: policy, FromSource: options.FromSource, NeedsXcode: needsXcode, HostMacPortsVersion: evaluation.Runtime.BaseVersion}
		config, err := p.Local.BuildConfig(ctx, platform, local)
		if err == nil {
			targets := map[string]record.BuildConfig{}
			if len(p.TargetImages) > 0 {
				binder, ok := p.Local.(LocalImages)
				if !ok {
					return workflow.BuildResolution{}, fmt.Errorf("Tart image selection is unavailable")
				}
				for _, name := range slices.Sorted(maps.Keys(p.TargetImages)) {
					if name == evaluation.Target.Name {
						return workflow.BuildResolution{}, fmt.Errorf("use --image for root target %s", name)
					}
					bound, err := binder.BuildConfigForImage(ctx, platform, local, p.TargetImages[name])
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
		if p.Name == "auto" {
			if errors.Is(err, verify.ErrImageUnavailable) {
				setup := "dockhand setup"
				if needsXcode {
					setup = "dockhand setup --xcode <archive-or-directory>"
				}
				progress.Report(ctx, "No suitable prepared Tart image is available; run %s to verify locally. Trying GitHub verification.", setup)
			} else if errors.Is(err, verify.ErrExecutableUnavailable) {
				progress.Report(ctx, "Tart is not available. Trying GitHub verification.")
			} else {
				return workflow.BuildResolution{}, err
			}
			resolved, remoteErr := remote()
			if remoteErr != nil && options.Preserve && ctx.Err() == nil {
				return workflow.BuildResolution{Problem: "GitHub verification could not be configured: " + remoteErr.Error()}, nil
			}
			if remoteErr == nil {
				progress.Report(ctx, "Using GitHub verification.")
			}
			return resolved, remoteErr
		}
		if len(p.TargetImages) > 0 {
			return workflow.BuildResolution{}, err
		}
		if options.Preserve {
			requirements := &record.BuildRequirements{Provider: verify.ProviderTart, Platform: platform, NeedsXcode: needsXcode, CapabilitiesRequired: true, Tests: policy, FromSource: options.FromSource}
			return workflow.BuildResolution{Requirements: requirements, Problem: err.Error()}, nil
		}
		return workflow.BuildResolution{}, err
	}
}
