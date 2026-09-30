package assess

import (
	"fmt"
	"go/version"
	"path"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// Toolchain is what the candidate's go.mod requires of a module-mode Go
// port's go.toolchain_min, and what an edit did about it.
type Toolchain struct {
	// Required is the go directive, as go.mod writes it.
	Required string
	// Declared is go.toolchain_min as the edit found it, and Outcome what
	// the edit did: covered, raised, undeclared, or by-hand; empty where
	// no edit looked.
	Declared, Outcome string
}

// The outcomes an edit reports.
const (
	ToolchainCovered    = "covered"
	ToolchainRaised     = "raised"
	ToolchainUndeclared = "undeclared"
	ToolchainByHand     = "by-hand"
)

// toolchain judges the candidate's go.toolchain_min against what its
// go.mod requires, whatever edit made it: a requirement the minimum
// doesn't gate on is one a passing build can't catch, since the builder's
// Go is new enough, and holds, unless the base's was below the same
// requirement already. It's said whatever it found, since silence reads
// the same as not having looked.
func (a *assessment) toolchain() (model.UpstreamChange, bool) {
	t := a.input.Toolchain
	if t == nil || t.Required == "" || !a.input.Port.GoModuleMode() {
		return model.UpstreamChange{}, false
	}
	final := a.input.Port.Options["go.toolchain_min"]
	covered := macports.GoToolchainCovers(final, t.Required)
	found := model.UpstreamChange{Kind: "toolchain", Path: "go.mod", Rule: GoToolchainRule, Subject: t.Required, Class: model.Introduced, Hold: !covered}
	outcome := t.Outcome
	if outcome == "" {
		switch {
		case covered:
			outcome = ToolchainCovered
		case final == "":
			outcome = ToolchainUndeclared
		default:
			outcome = ToolchainByHand
		}
	}
	switch outcome {
	case ToolchainCovered:
		found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, which go.toolchain_min %s already gates on", t.Required, final)
	case ToolchainRaised:
		found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, so go.toolchain_min is raised from %s", t.Required, t.Declared)
	case ToolchainUndeclared:
		found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, and the Portfile declares no go.toolchain_min; declaring one gates the port on older Go, the maintainer's call", t.Required)
	default:
		found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, above go.toolchain_min %s, which isn't one literal declaration dockhand can raise; raise it by hand", t.Required, final)
	}
	if covered {
		return found, true
	}
	switch required, known := a.baseRequired(); {
	case !known:
		found.Class = model.UnknownBaseline
	case a.input.Base.GoModuleMode() && !macports.GoToolchainCovers(a.input.Base.Options["go.toolchain_min"], required) &&
		version.Compare(version.Lang("go"+required), version.Lang("go"+t.Required)) >= 0:
		// The base required as much, and didn't gate on it either.
		found.Class, found.Hold = model.Present, false
		found.Message += "; the base's didn't gate on it either"
	}
	return found, true
}

// baseRequired is what the base's go.mod required, from the archives
// read, which hold only the project's own at its root; false where no
// archive holds one it could read.
func (a *assessment) baseRequired() (string, bool) {
	for _, pair := range a.input.Pairs {
		for name, file := range pair.Before.Files {
			if path.Base(name) != "go.mod" || file.Truncated {
				continue
			}
			if module, err := project.ReadGoMod(file.Data); err == nil && module.Go != "" {
				return module.Go, true
			}
		}
	}
	return "", false
}
