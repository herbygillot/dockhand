package assess

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/model"
)

// Patch is a patch's verdict against the candidate's source, as MacPorts
// would apply it: one of the candidate's own, or one the base applied that
// the candidate doesn't (Dropped).
type Patch struct {
	patchcheck.Result
	Dropped bool
}

// patches are the candidate's patches that don't apply to its source, and
// the base's it drops, with whether they still would: libuv's #34620
// dropped patch-libuv-legacy.diff, which no longer applied to 1.52.1 in
// five files, and its review said nothing of it (the libuv run's finding
// 2). Neither holds: a patch that doesn't apply fails the build, which a
// check catches, and dropping one is its author's to explain. A patch the
// check couldn't model is coverage, not a finding.
func (a *assessment) patches() []model.UpstreamChange {
	source := "the new version's source"
	if a.input.Versions.New != "" {
		source = a.input.Versions.New + "'s source"
	}
	var found []model.UpstreamChange
	for _, patch := range a.input.Patches {
		if !patch.Checked {
			a.cover(model.Coverage{Path: patch.Name, Relevance: "used", Treatment: "set-apart", Policy: "patch-unchecked", Reason: patch.Detail})
			continue
		}
		change := model.UpstreamChange{Kind: "patch", Path: patch.Name, Subject: patch.Name, Class: model.Introduced}
		switch {
		case patch.Dropped && patch.Applies:
			change.Rule = PatchDropped
			change.Message = fmt.Sprintf("%s, which the base applied, is dropped, though it still applies to %s: what it fixed may need it still", patch.Name, source)
		case patch.Dropped:
			change.Rule = PatchDropped
			change.Message = fmt.Sprintf("%s, which the base applied, is dropped, and no longer applies to %s: %s", patch.Name, source, patch.Detail)
		case !patch.Applies:
			change.Rule = PatchRejected
			change.Message = fmt.Sprintf("%s doesn't apply to %s, so the build fails at its patch phase: %s", patch.Name, source, patch.Detail)
		default:
			// One that applies is coverage, said so a check that found
			// nothing isn't taken for none: fluent-bit's six applied, and
			// the person dry-ran them by hand (the fluent-bit run).
			a.cover(model.Coverage{Path: patch.Name, Relevance: "used", Treatment: "inspected", Policy: "patch-applies", Reason: "applies to " + source})
			continue
		}
		found = append(found, change)
	}
	return found
}
