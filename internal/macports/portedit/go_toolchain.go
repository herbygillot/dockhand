package portedit

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
	"golang.org/x/mod/semver"
)

// The Go PortGroup's go.toolchain_min gates a port on systems whose Go is
// too old. Which value is right depends on how the port builds, and the
// PortGroup's own guidance is followed here: with go.offline_build no the
// build runs in module mode, Go enforces go.mod's go directive, and the
// directive is exactly the minimum, so it is copied; in GOPATH mode the
// directive is only an upper bound on what the source needs, so the
// declared minimum is left alone.

// moduleModeGo reports a Go PortGroup port that builds in module mode.
func moduleModeGo(info macports.PortInfo) bool {
	return info.Options["go.package"] != "" && info.Options["go.offline_build"] == "no"
}

// raiseGoToolchain reads the new release's go.mod from the kept archive and
// raises a literal go.toolchain_min to the series it requires. It never
// lowers one, never adds one, and never refuses the bump: a port that
// declares no minimum, or carries it in a way that cannot be edited, is
// told about the requirement and left as it is.
func (s *Service) raiseGoToolchain(ctx context.Context, input *sourceInput, result *Result) error {
	if len(result.Files) == 0 || len(result.Fidelity) == 0 {
		return nil
	}
	previous := result.Fidelity[len(result.Fidelity)-1].After
	selected := previous.Ports[input.target.Name]
	if selected.Options["go.package"] == "" {
		return nil
	}
	if !moduleModeGo(selected) {
		progress.VerboseReport(ctx, "%s builds in GOPATH mode, where go.mod's go directive is only an upper bound; go.toolchain_min is left as declared", input.target.Name)
		return nil
	}
	required, found, err := s.goRequirement(ctx, selected, result.Downloads)
	if err != nil {
		return err
	}
	if !found || required == "" {
		return nil
	}
	current := selected.Options["go.toolchain_min"]
	switch {
	case current == "":
		progress.Report(ctx, "%s requires Go %s per go.mod and declares no go.toolchain_min; declaring it would gate the port on systems whose Go is older", input.target.Name, required)
		return nil
	case semver.Compare("v"+semver.MajorMinor("v" + current)[1:], "v"+required) >= 0:
		progress.VerboseReport(ctx, "go.toolchain_min %s already covers the %s that go.mod requires", current, required)
		return nil
	}
	contents, err := rewriteLiteralDeclaration(result.Files[0].After, "go.toolchain_min", current, required)
	if errors.Is(err, ErrUnsupported) {
		progress.Report(ctx, "Warning: %s requires Go %s per go.mod but go.toolchain_min %s is not a single literal declaration; raise it by hand", input.target.Name, required, current)
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
		report.UnexpectedChanges = append(report.UnexpectedChanges, fidelity.Compare(name, fidelity.ComparablePort(expected, input.files.root), fidelity.ComparablePort(next, input.files.root))...)
	}
	result.Files = []portfile.Edit{evaluated.edit}
	result.Fidelity = append(result.Fidelity, report)
	if len(report.UnexpectedChanges) > 0 {
		return fmt.Errorf("%w: %v", ErrFidelity, report.UnexpectedChanges)
	}
	progress.Report(ctx, "Raising go.toolchain_min from %s to %s, which %s's go.mod requires", current, required, input.target.Name)
	return nil
}

// goRequirement finds go.mod in the first kept archive that holds one at
// the port's worksrcdir and reports the Go series it requires.
func (s *Service) goRequirement(ctx context.Context, info macports.PortInfo, downloads []Download) (required string, found bool, err error) {
	for _, download := range downloads {
		if download.path == "" {
			continue
		}
		data, _, err := dependency.Manifest(ctx, download.path, info.Options["worksrcdir"], "go.mod")
		if errors.Is(err, dependency.ErrManifestMissing) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		required, err := dependency.GoRequirement(data)
		if err != nil {
			return "", false, fmt.Errorf("%w: reading go.mod: %v", ErrUnsupported, err)
		}
		return required, true, nil
	}
	return "", false, nil
}
