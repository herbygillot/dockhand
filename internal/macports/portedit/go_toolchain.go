package portedit

import (
	"context"
	"errors"
	"fmt"
	"go/version"
	"maps"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/project"
)

// The Go PortGroup's go.toolchain_min gates a port on systems whose Go is
// too old. Which value is right depends on how the port builds, and the
// PortGroup's own guidance is followed here: with go.offline_build no the
// build runs in module mode, Go enforces go.mod's go directive, and the
// directive is exactly the minimum, so it is copied; in GOPATH mode the
// directive is only an upper bound on what the source needs, so the
// declared minimum is left alone.

// moduleModeGo reports a Go PortGroup port that builds in module mode:
// go.offline_build set and false, read as Tcl reads a boolean. An unset or
// unreadable value is not module mode, which leaves the minimum alone.
func moduleModeGo(info macports.PortInfo) bool {
	if _, set := info.Options["go.offline_build"]; !set || info.Options["go.package"] == "" {
		return false
	}
	offline, err := info.Bool("go.offline_build")
	return err == nil && !offline
}

// raiseGoToolchain reads the new release's go.mod, from the kept archive or,
// for a git-fetched port, from the repository at the resolved commit, and
// raises a literal go.toolchain_min below the series it requires to the
// release it names, as go.mod writes it. It never lowers one, never adds
// one, and never refuses the bump: a port that declares no minimum, or
// carries it in a way that cannot be edited, is told about the requirement
// and left as it is. What it found and did is the result's GoToolchain.
func (s *Service) raiseGoToolchain(ctx context.Context, request Request, input *sourceInput, result *Result) error {
	if len(result.Files) == 0 || result.Prepared.Ports == nil {
		return nil
	}
	previous := result.Prepared
	selected := previous.Ports[input.target.Name]
	if selected.Options["go.package"] == "" {
		return nil
	}
	if !moduleModeGo(selected) {
		progress.VerboseReport(ctx, "%s builds in GOPATH mode, where go.mod's go directive is only an upper bound; go.toolchain_min is left as declared", input.target.Name)
		return nil
	}
	required, found, err := s.goRequirement(ctx, request, selected, result.Downloads)
	if err != nil {
		return err
	}
	if !found || required == "" {
		return nil
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
	case seriesCovers(current, required):
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
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return err
	}
	after := evaluated.after
	if after.Ports[input.target.Name].Options["go.toolchain_min"] != required {
		return fmt.Errorf("%w: evaluated go.toolchain_min differs from the required %s", ErrFidelity, required)
	}
	report := Fidelity{Before: previous, After: after, ExpectedChanges: []string{input.target.Name + ".go.toolchain_min -> " + required}}
	for name, old := range previous.Ports {
		next, ok := after.Ports[name]
		if !ok {
			return fmt.Errorf("%w: raising go.toolchain_min changed the port set", ErrFidelity)
		}
		expected := old
		if name == input.target.Name {
			expected.Options = maps.Clone(old.Options)
			expected.Options["go.toolchain_min"] = required
		}
		report.UnexpectedChanges = append(report.UnexpectedChanges, fidelity.Compare(name, fidelity.ComparablePort(expected, previous.Root), fidelity.ComparablePort(next, after.Root))...)
	}
	result.Files = []portfile.Edit{evaluated.edit}
	result.report(report)
	if len(report.UnexpectedChanges) > 0 {
		return fmt.Errorf("%w: %v", ErrFidelity, report.UnexpectedChanges)
	}
	progress.Report(ctx, "Raising go.toolchain_min from %s to %s, which %s's go.mod requires", current, required, input.target.Name)
	outcome(GoToolchainRaised)
	return nil
}

// seriesCovers reports whether a declared go.toolchain_min is of the
// required release's series or a later one. The Go PortGroup compares only
// the series, since MacPorts ships the newest patch release of each series
// it packages, so 1.26 already gates on what go.mod's 1.26.8 asks, and a
// patch release moving within the series leaves the Portfile alone. A
// declared value Go can't read covers nothing.
func seriesCovers(declared, required string) bool {
	series := version.Lang("go" + declared)
	return series != "" && version.Compare(series, version.Lang("go"+required)) >= 0
}

// goRequirement finds go.mod in the first kept archive that holds one at
// the port's worksrcdir, or in the repository at the resolved commit when
// the port is fetched with git, and reports the Go release it requires.
func (s *Service) goRequirement(ctx context.Context, request Request, info macports.PortInfo, downloads []archives.Download) (required string, found bool, err error) {
	if gitFetched(info) {
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
		data, _, err := dependency.Manifest(ctx, download.Path, info.Options["worksrcdir"], "go.mod")
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
