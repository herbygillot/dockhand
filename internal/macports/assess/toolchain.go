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

// The outcomes an edit reports, and toolchainBelow, a minimum below the
// requirement that no edit looked at.
const (
	toolchainBelow      = "below"
	ToolchainCovered    = "covered"
	ToolchainRaised     = "raised"
	ToolchainUndeclared = "undeclared"
	ToolchainByHand     = "by-hand"
)

// toolchain judges the candidate's go.toolchain_min, and the Go it pins
// its build to, against what its go.mod requires, whatever edit made them.
// A requirement the minimum doesn't gate on is one a passing build can't
// catch, since the builder's Go is new enough, and holds, unless the
// base's was below the same requirement already. A pin older than the
// requirement holds too, unless the base had the same pin and its go.mod
// already asked more of it: trivy pinned go-1.26, and 0.75.0's go.mod
// required 1.27.0, while the minimum, raised to 1.27.0, read as all being
// well (the trivy run, #35083). A check builds with the pin and fails, so
// this is said before it is; the pin may have been for the old version
// alone, as trivy's was. It's said whatever it found, since silence reads
// the same as not having looked.
func (a *assessment) toolchain() (model.UpstreamChange, bool) {
	t := a.input.Toolchain
	if t == nil {
		// Where no edit said, the new version's go.mod says, as read.
		if required, ok := goRequired(a.input.Pairs, after); ok {
			t = &Toolchain{Required: required}
		}
	}
	if t == nil || t.Required == "" || !a.input.Port.GoModuleMode() {
		return model.UpstreamChange{}, false
	}
	final := a.input.Port.Options["go.toolchain_min"]
	covered := macports.GoToolchainCovers(final, t.Required)
	pin, pinned := a.input.Port.GoPinned()
	behind := pinned && !pin.Meets(t.Required)
	found := model.UpstreamChange{Kind: "toolchain", Path: "go.mod", Rule: GoToolchainRule, Subject: t.Required, Class: model.Introduced, Hold: !covered || behind}
	outcome := t.Outcome
	if outcome == "" {
		switch {
		case covered:
			outcome = ToolchainCovered
		case final == "":
			outcome = ToolchainUndeclared
		default:
			outcome = toolchainBelow
		}
	}
	switch outcome {
	case ToolchainCovered:
		found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, which go.toolchain_min %s already gates on", t.Required, final)
	case ToolchainRaised:
		found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, so go.toolchain_min is raised from %s", t.Required, t.Declared)
	case ToolchainUndeclared:
		found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, and the Portfile declares no go.toolchain_min; declaring one gates the port on older Go, the maintainer's call", t.Required)
	case ToolchainByHand:
		found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, above go.toolchain_min %s, which isn't one literal declaration dockhand can raise; raise it by hand", t.Required, final)
	default:
		found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, above go.toolchain_min %s, which doesn't gate on it", t.Required, final)
	}
	if covered && !behind {
		return found, true
	}
	// Each concern is weighed against the base, and the finding holds
	// where either is the candidate's own, or the base couldn't be read.
	minimumPresent, pinPresent := false, false
	required, known := goRequired(a.input.Pairs, before)
	if known && a.input.Base.GoModuleMode() {
		// The base required as much, and didn't gate on it either.
		minimumPresent = !covered && !macports.GoToolchainCovers(a.input.Base.Options["go.toolchain_min"], required) &&
			version.Compare(version.Lang("go"+required), version.Lang("go"+t.Required)) >= 0
		// The base pinned the same Go, which its own go.mod already asked
		// more of.
		was, ok := a.input.Base.GoPinned()
		pinPresent = behind && ok && was.Series == pin.Series && !was.Meets(required)
	}
	if minimumPresent {
		found.Message += "; the base's didn't gate on it either"
	}
	if behind {
		// Where the minimum gates on the requirement, the pin is what's
		// wrong, and "already gates on" mustn't read as all being well.
		says := fmt.Sprintf("the Portfile pins %s; the pin may be obsolete", pin)
		switch {
		case outcome == ToolchainCovered && covered:
			found.Message = fmt.Sprintf("upstream: go.mod requires Go %s, but %s", t.Required, says)
		case outcome == ToolchainRaised:
			found.Message += ", but " + says
		default:
			found.Message += "; and " + says
		}
		if pinPresent {
			found.Message += "; the base's pin didn't meet its go.mod either"
		}
	}
	switch {
	case !known:
		found.Class = model.UnknownBaseline
	case (covered || minimumPresent) && (!behind || pinPresent):
		found.Class, found.Hold = model.Present, false
	}
	return found, true
}

// goRequired is what a version's go.mod requires, from the archives read,
// which hold only the project's own at its root; false where no archive
// holds one it could read.
func goRequired(pairs []Pair, side func(Pair) project.Reading) (string, bool) {
	for _, pair := range pairs {
		for name, file := range side(pair).Files {
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
