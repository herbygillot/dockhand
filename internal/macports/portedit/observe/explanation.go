package observe

import (
	"context"
	"github.com/herbygillot/dockhand/internal/macports"
)

// Rule names the first rule that explains an access, or none. Names are
// persisted by the survey; changing their meaning requires a record version.
type Rule string

const (
	RuleNone         Rule = "none"
	RuleBuildDefault Rule = "build-default"
	RuleBaseLookup   Rule = "base-lookup"
	RuleSourceDate   Rule = "source-date-epoch"
	RuleCallback     Rule = "callback"
	RuleToolchain    Rule = "toolchain"
	RulePortGroup    Rule = "portgroup"
)

type FollowerUse struct {
	Option  string
	Watched []string
	Benign  bool
}

type AccessExplanation struct {
	Declaration int
	Ignored     bool `json:",omitempty"`
	Rule        Rule
	Followers   []FollowerUse `json:",omitempty"`
}

// Explanation diagnoses every counted host access, even after a failure.
// Observation and Inconclusive are exactly Tolerate's result; the original
// input remains immutable. Unmatched contains host problems with no access.
type Explanation struct {
	Observation  macports.PortObservation `json:"-"`
	Inconclusive bool
	Accesses     []AccessExplanation `json:",omitempty"`
	Unmatched    []string            `json:",omitempty"`
}

func Explain(ctx context.Context, port macports.PortObservation, contents []byte, root string) Explanation {
	var trace Explanation
	if !(port.HostAccess || port.ModeledHostAccess) {
		trace.Observation = port
		for i, d := range port.Declarations {
			if d.Command == "dockhand.host-access" && len(d.Values) > 0 {
				trace.Accesses = append(trace.Accesses, AccessExplanation{Declaration: i, Rule: RuleNone, Ignored: true})
			}
		}
		return trace
	}
	trace.Observation, trace.Inconclusive = tolerate(ctx, port, contents, root, &trace)
	return trace
}

func followHostReads(base *baseCode, sources [][]byte, option string) ([]string, bool) {
	watched, benign := followValue(base.context, sources, option, base)
	if base.recordFollow {
		base.followed = append(base.followed, FollowerUse{Option: option, Watched: watched, Benign: benign})
	}
	return watched, benign
}
