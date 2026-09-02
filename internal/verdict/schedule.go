package verdict

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Preflight is what one host-side evaluation answered about a portdir
// under a release's platform frame, before any VM booted: does the port
// refuse this platform, and does it need a capability — full Xcode — a
// base image may not have.
//
// The evaluation is the caller's; this is only its answer. The zero
// value is "not evaluated", which reads as "build it", because a
// preflight that could not run is not evidence that a port declines
// anything.
type Preflight struct {
	KnownFail bool
	UseXcode  bool
}

// Scheduled is one platform's disposition before any worker boots.
type Scheduled struct {
	Release platform.Release
	// NeedsXcode rides with the submission so a port that needs a full
	// Xcode is matched to a base that has one, rather than discovering
	// the gap forty minutes into a build.
	NeedsXcode bool
	// Declined is the run to record INSTEAD of building, when the port
	// declares known_fail on this release. nil means submit.
	//
	// Recording rather than skipping is the point: a platform a port
	// refuses is a real verdict about that platform, and status showing
	// it beats status showing nothing and the user wondering.
	Declined *record.Run
	// Message is what to tell the user about a declined platform, on
	// stderr. Empty when the platform will be built.
	Message string
}

// ResolveRelease picks the release a run lands on when the caller named
// none: the provider's first base, which the VM provider orders newest
// first.
//
// It resolves before anything is recorded, because a run is keyed by
// release name and "the default" is not a key. A provider with no bases
// at all is the no-environment refusal rather than an index past the end
// of an empty list — no provider today reports an empty list, so the
// refusal is the guard for one that does.
func ResolveRelease(want platform.Release, available []platform.Release) (platform.Release, error) {
	if !want.IsZero() {
		return want, nil
	}
	if len(available) == 0 {
		return platform.Release{}, fmt.Errorf("%w: no base images", verify.ErrNoEnvironment)
	}
	return available[0], nil
}

// SchedulePlatforms says, for each release a submission covers, whether
// the port will actually be built on it — mpbb's list-time exclusion,
// borrowed: the buildbot drops known_fail ports before the build rather
// than discovering the refusal mid-build, and evaluation answers in a
// second where a VM takes an hour.
//
// pre holds one Preflight per release name. A release with no entry was
// not evaluated — the evaluation failed, and the caller warned about it
// — and is scheduled as an ordinary build, which is what a machine that
// could not ask should do.
func SchedulePlatforms(port string, releases []platform.Release, pre map[string]Preflight) []Scheduled {
	out := make([]Scheduled, 0, len(releases))
	for _, r := range releases {
		pf := pre[r.Name]
		s := Scheduled{Release: r, NeedsXcode: pf.UseXcode}
		if pf.KnownFail {
			s.Declined = &record.Run{
				State:  record.Unsupported,
				Detail: "declares known_fail on " + r.Name,
			}
			s.Message = fmt.Sprintf("%s declares known_fail on %s; recorded unsupported — no build attempted", port, r.Name)
		}
		out = append(out, s)
	}
	return out
}
