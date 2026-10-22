package portedit

import (
	"context"
	"maps"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
)

// correction is one of dockhand's corrections to the edit it prepared:
// raising go.toolchain_min, a stealth update's revision and dist_subdir,
// dropping a stealth update's dist_subdir, or dropping patches upstream
// merged. It's the Portfile as corrected, the files it deletes beside it,
// and the change it's allowed, against which the whole family is held
// (Codex's review of 832f059e, high 3: patch removal checked the patch
// list, the version, and the revision alone, and replaced Prepared).
type correction struct {
	contents []byte
	deletes  []string
	// expected names the allowed change, for the report.
	expected []string
	// allowed is a port as prepared with the change it's allowed made,
	// given the port as corrected; every port, every option, and each
	// revision is compared against it. Nil allows a port no change.
	allowed func(name string, prepared, corrected macports.PortInfo) macports.PortInfo
}

// correct evaluates a correction against the prepared snapshot and holds
// the whole family to its allowed change. Only where nothing else moved
// does it publish the files, the snapshot, and the report together, so no
// correction replaces Prepared without its check; applied says whether it
// did. The report comes back either way, for a caller that says what else
// would have changed.
func (s *Service) correct(ctx context.Context, input *sourceInput, result *Result, c correction) (Fidelity, bool, error) {
	previous := result.Prepared
	evaluated, err := s.evaluateEdit(ctx, input, c.contents)
	if err != nil {
		return Fidelity{}, false, err
	}
	report := correctionReport(previous, evaluated.after, c)
	if len(report.UnexpectedChanges) > 0 {
		return report, false, nil
	}
	result.Files[0] = evaluated.edit
	for _, file := range c.deletes {
		result.Files = append(result.Files, portfile.Edit{Path: file, Delete: true})
	}
	result.report(report)
	return report, true, nil
}

// correctionReport compares each port as corrected with the port as
// prepared, its allowed change made: the port set, each revision, and
// every option fidelity compares.
func correctionReport(before, after macports.Snapshot, c correction) Fidelity {
	prediction := fidelity.Predict(before)
	prediction.Expected = c.expected
	if c.allowed != nil {
		for name, old := range before.Ports {
			next, ok := after.Ports[name]
			if !ok {
				continue
			}
			expected := old
			expected.Options = maps.Clone(old.Options)
			if expected.Options == nil {
				expected.Options = map[string]string{}
			}
			prediction.Expect(name, fidelity.ComparablePort(c.allowed(name, expected, next), before.Root))
		}
	}
	return prediction.Observe(before, after)
}
