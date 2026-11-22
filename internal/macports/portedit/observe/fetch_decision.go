package observe

import (
	"context"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
)

// FetchDecisionKind is the result of deciding whether an observed context
// can contribute a fetch plan.
type FetchDecisionKind uint8

const (
	FetchUsable FetchDecisionKind = iota
	FetchLeft
	FetchRefused
	FetchInconclusive
)

// FetchNeed names how far the caller goes in the ordered decision. Revision
// readers decide only whether to leave; plan readers also apply archive
// policy and read the plan; judged-plan readers additionally judge whether
// host access makes that plan inconclusive.
type FetchNeed uint8

const (
	FetchPlanNeed FetchNeed = iota
	FetchRevisionOnly
	FetchJudgedPlan
)

// FetchDecision is the ordered decision used by readers of a context. Usable
// plan decisions carry the evaluated plan; judged-plan decisions also carry
// tolerated metadata. Err carries a policy refusal or a plan evaluation error.
type FetchDecision struct {
	Kind     FetchDecisionKind
	Reason   string
	Err      error
	Info     macports.PortInfo
	Metadata macports.PortObservation
	Plan     []macports.Distfile
}

// DecideFetch orders the questions callers already ask on main: artifact
// assessment and archive binding use FetchJudgedPlan (left, policy, verdict,
// plan); shipped-plan and treequery readers use FetchPlanNeed (left, policy,
// plan); revision-only readers use FetchRevisionOnly (left). When load is
// set, it obtains the native observation only after policy allows the fetch,
// so a refusal doesn't require a session.
func DecideFetch(ctx context.Context, observed macports.Observation, port string, contents []byte, portdir string, need FetchNeed, load func(context.Context) (macports.Observation, error)) FetchDecision {
	reason, left, metadata, checked := left(ctx, observed, port, contents)
	if left {
		return FetchDecision{Kind: FetchLeft, Reason: reason, Info: observed.Snapshot.Ports[port]}
	}
	info := observed.Snapshot.Ports[port]
	if need == FetchRevisionOnly {
		return FetchDecision{Kind: FetchUsable, Info: info, Metadata: metadata}
	}
	// A loaded native observation is requested specifically to fetch its plan,
	// so check policy before loading it. For an already observed context, an
	// empty plan has no archive policy to apply.
	if load != nil || len(observed.Ports[port].Distfiles) > 0 {
		if err := distfetch.CheckPolicy(info, portdir); err != nil {
			return FetchDecision{Kind: FetchRefused, Err: err, Info: info}
		}
	}
	if load != nil {
		var err error
		observed, err = load(ctx)
		if err != nil {
			return FetchDecision{Kind: FetchInconclusive, Err: err, Info: info}
		}
		metadata, checked = macports.PortObservation{}, false
	}
	inconclusive := false
	if need == FetchJudgedPlan && !checked {
		metadata, inconclusive = Tolerate(ctx, observed.Ports[port], contents, observed.Snapshot.Root)
	}
	if need == FetchJudgedPlan && inconclusive {
		return FetchDecision{Kind: FetchInconclusive, Reason: "modeled context depends on host state" + HostInputs(metadata), Info: info, Metadata: metadata}
	}
	plan, err := observed.Ports[port].FetchPlan()
	if err != nil {
		if len(observed.Ports[port].Problems) == 0 {
			return FetchDecision{Kind: FetchUsable, Info: info, Metadata: metadata}
		}
		return FetchDecision{Kind: FetchInconclusive, Err: err, Info: info, Metadata: metadata}
	}
	return FetchDecision{Kind: FetchUsable, Info: info, Metadata: metadata, Plan: plan}
}
