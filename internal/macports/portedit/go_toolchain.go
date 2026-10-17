package portedit

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/project"
)

// raiseGoToolchain reads the new release's go.mod, from the kept archive or,
// for a git-fetched port, from the repository at the resolved commit, and
// raises a literal go.toolchain_min below the series it requires to the
// release it names, as go.mod writes it. It never lowers one, never adds
// one, and never refuses the bump: a port that declares no minimum, or
// carries it in a way that cannot be edited, is told about the requirement
// and left as it is, and a go.mod it can't read is said as unchecked. What
// it found and did is the result's GoToolchain.
func (s *Service) raiseGoToolchain(ctx context.Context, request Request, input *sourceInput, result *Result) error {
	if len(result.Files) == 0 || result.Prepared.Ports == nil {
		return nil
	}
	previous := result.Prepared
	selected := previous.Ports[input.target.Name]
	if selected.Options["go.package"] == "" {
		return nil
	}
	if !selected.GoModuleMode() {
		progress.VerboseReport(ctx, "%s builds in GOPATH mode, where go.mod's go directive is only an upper bound; go.toolchain_min is left as declared", input.target.Name)
		return nil
	}
	required, found, err := s.goRequirement(ctx, request, selected, result.Downloads)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		// A go.mod it can't read, as a duplicate member or a forge's error,
		// failed the update in depblock's words, where the raise never
		// refuses the bump (CX-9's narrow part): it's a notice that the
		// minimum wasn't checked, which holds an unattended update for a
		// look.
		progress.Report(ctx, "Warning: %s's go.mod couldn't be read, so go.toolchain_min wasn't checked against it: %v", input.target.Name, err)
		result.GoToolchain = &GoToolchain{Declared: selected.Options["go.toolchain_min"], Outcome: GoToolchainUnchecked, Problem: err.Error()}
		return nil
	}
	if !found || required == "" {
		return nil
	}
	// A pin older than the requirement fails the build whatever the
	// minimum says: trivy's go-1.26 under 0.75.0's Go 1.27.0, where only
	// the raise was said (the trivy run, #35083). The pin is the
	// maintainer's to remove, and is left as it is.
	if pin, ok := selected.GoPinned(); ok && !pin.Meets(required) {
		progress.Report(ctx, "Warning: %s requires Go %s per go.mod, but pins %s; the pin may be obsolete", input.target.Name, required, pin)
	}
	current := selected.Options["go.toolchain_min"]
	outcome := func(outcome GoToolchainOutcome) {
		result.GoToolchain = &GoToolchain{Required: required, Declared: current, Outcome: outcome}
	}
	switch {
	case current == "":
		progress.Report(ctx, "%s requires Go %s per go.mod and declares no go.toolchain_min; declaring it would gate the port on systems whose Go is older", input.target.Name, required)
		outcome(GoToolchainUndeclared)
		return nil
	case macports.GoToolchainCovers(current, required):
		progress.VerboseReport(ctx, "The Portfile's go.toolchain_min %s already covers the %s that go.mod requires", current, required)
		outcome(GoToolchainCovered)
		return nil
	}
	contents, err := portfile.RewriteLiteralDeclaration(result.Files[0].After, "go.toolchain_min", current, required)
	if errors.Is(err, ErrUnsupported) {
		progress.Report(ctx, "Warning: %s requires Go %s per go.mod but go.toolchain_min %s is not a single literal declaration; raise it by hand", input.target.Name, required, current)
		outcome(GoToolchainByHand)
		return nil
	}
	if err != nil {
		return err
	}
	report, applied, err := s.correct(ctx, input, result, correction{contents: contents, expected: []string{input.target.Name + ".go.toolchain_min -> " + required},
		allowed: func(name string, prepared, _ macports.PortInfo) macports.PortInfo {
			if name == input.target.Name {
				prepared.Options["go.toolchain_min"] = required
			}
			return prepared
		}})
	if err != nil {
		return err
	}
	if !applied {
		return report.Err()
	}
	progress.Report(ctx, "Raising go.toolchain_min from %s to %s, which %s's go.mod requires", current, required, input.target.Name)
	outcome(GoToolchainRaised)
	return nil
}

// goRequirement finds go.mod in the first kept archive that holds one at
// the port's worksrcdir, or in the repository at the resolved commit when
// the port is fetched with git, and reports the Go release it requires.
func (s *Service) goRequirement(ctx context.Context, request Request, info macports.PortInfo, downloads []distfetch.Download) (required string, found bool, err error) {
	if info.GitFetched() {
		if s.Manifests == nil || request.Release == nil {
			progress.VerboseReport(ctx, "%s is fetched with git and no manifest source is configured; go.toolchain_min is left as declared", info.Name)
			return "", false, nil
		}
		data, err := s.Manifests.Manifest(ctx, info, *request.Release, "go.mod")
		if errors.Is(err, macports.ErrManifestMissing) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		module, err := project.ReadGoMod(data)
		if err != nil {
			return "", false, fmt.Errorf("%w: reading go.mod: %v", ErrUnsupported, err)
		}
		return module.Go, true, nil
	}
	for _, download := range downloads {
		if download.Path == "" {
			continue
		}
		data, _, err := depblock.Manifest(ctx, download.Path, info.Options["worksrcdir"], "go.mod")
		if errors.Is(err, macports.ErrManifestMissing) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		module, err := project.ReadGoMod(data)
		if err != nil {
			return "", false, fmt.Errorf("%w: reading go.mod: %v", ErrUnsupported, err)
		}
		return module.Go, true, nil
	}
	return "", false, nil
}
