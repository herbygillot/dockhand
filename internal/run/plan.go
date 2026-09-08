package run

import (
	"errors"
	"slices"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// ErrNothingToBuild is Plan's refusal of a spec whose every member has
// already been answered: each one withheld, or declining this platform
// at preflight. It is not an error about the port and it is not a
// failure of the attempt — the verdicts Plan returned beside it are the
// answer, and Start records them and stops.
//
// It is a sentinel and not an empty request, because "build nothing" and
// "there was nothing to build" are the same value otherwise, and a
// provider handed an empty Ports slice would boot a guest to run `port`
// with no arguments.
var ErrNothingToBuild = errors.New("run: every member is answered without a build")

// Plan is the pre-submission decision: which members are actually
// going to be built, which decline before a VM boots, and what the
// provider request looks like. It reads the preflight, so a port
// declaring known_fail is answered here on every road.
//
// mpbb's list-time exclusion, borrowed: the buildbot drops known_fail
// ports before the build rather than discovering the refusal mid-build,
// and a host-side evaluation answers in a second where a VM takes an
// hour. That is the fix for a synchronous gate that booted a VM to
// discover known_fail, and it is why a bump on a permanently-failing
// port declines in seconds.
//
// THE RUNS IT RETURNS ARE RECORDED INSTEAD OF BUILDING, and recording
// rather than skipping is the point: a platform a port refuses is a real
// verdict about that platform, and a record showing it beats a record
// showing nothing and the user wondering. Two shapes reach it — a member
// the roster withheld (record.Withheld, the sentence the cohort gave)
// and a member that declares known_fail here (record.Unsupported) — and
// both are terminal, so Judge will not write over them.
//
// THEY COME BACK KEYED BY MEMBER PORT and not as the sketch's bare
// slice, and the correction is forced by the record rather than
// preferred: record.Run carries no port of its own. The port is the
// KEY, in record.Attempt.Runs and in the note's projection alike, so a
// slice of runs cannot say which member a decline is about — Start would
// have had to re-derive the pairing by walking the roster in the same
// order this function happened to build it, which is a parallel-slice
// contract nothing enforces. A map is the shape the value is written
// into at the other end.
//
// A PREFLIGHT THAT COULD NOT BE READ IS NOT A DECLINE. Preflight.Read is
// rule 7 on that struct precisely so this function can tell "the port
// does not declare known_fail" from "the staged Portfile could not be
// read", and an unread member is scheduled as an ordinary build: a
// machine that could not ask has learned nothing about the port. What it
// costs is stated where a person will meet it rather than swallowed
// here, because a check that did not happen is news and a judgment about
// the platform is not the place to say so.
//
// WHERE THAT IS was wrong in this doc for the whole of the overhaul. It
// said "Start warns from Preflight.Err", and nothing in the tree read
// Preflight.Err at all — no warning, no record, and an Evidence.Preflight
// field that nothing filled. So a Portfile that would not evaluate bought
// a VM, forty minutes and a bare FAILED, with the one fact that explained
// it discarded at this line. Start now writes the failures onto the
// attempt (record.Attempt.Unchecked), Finish carries them to Judge, and
// stamp puts the sentence beside the verdict that paid for it — hours
// later and usually in another process, which is why the fact has to be
// durable rather than printed.
//
// The request's parallel slices — Requires and Deactivate — are built
// over the ports ACTUALLY BEING BUILT and never over the roster, because
// a member that declined the platform is not in the request and a graph
// drawn over the roster would name positions the guest does not have.
func Plan(spec Spec, pre map[string]Preflight) (verify.Request, map[string]record.Run, error) {
	return plan(spec, pre, nil, "")
}

// PlanWith is Plan with the baseline the stager materialized, which is
// the input the ABI comparison exists to read and which nothing used to
// supply.
//
// note is why there is no baseline when there is none — see
// verify.Request.BaselineNote. It is a second parameter rather than a
// sentinel inside the slice because an empty baseline and a FAILED
// baseline are two facts, and the whole cost of this path was one being
// read as the other.
func PlanWith(spec Spec, pre map[string]Preflight, baseline []string, note string) (verify.Request, map[string]record.Run, error) {
	return plan(spec, pre, baseline, note)
}

func plan(spec Spec, pre map[string]Preflight, baseline []string, note string) (verify.Request, map[string]record.Run, error) {
	runs := make(map[string]record.Run, len(spec.Withheld)+len(spec.Roster))
	for _, w := range spec.Withheld {
		runs[w.Port] = record.Run{
			State: record.Withheld, Content: spec.Content, Detail: w.Why,
		}
	}
	seated := make([]Member, 0, len(spec.Roster))
	needsXcode := false
	for _, m := range spec.Roster {
		pf := pre[m.Port]
		if pf.Read && pf.KnownFail {
			runs[m.Port] = record.Run{
				State: record.Unsupported, Content: spec.Content,
				Detail: declineDetail(pf, spec),
			}
			continue
		}
		needsXcode = needsXcode || pf.NeedsXcode
		seated = append(seated, m)
	}
	if len(seated) == 0 {
		return verify.Request{}, runs, ErrNothingToBuild
	}
	ports := memberPorts(seated)
	portdirs := make([]string, 0, len(seated))
	for _, m := range seated {
		portdirs = append(portdirs, m.Portdir)
	}
	req := verify.Request{
		Ports:      ports,
		Portdirs:   portdirs,
		Platform:   spec.Platform,
		Test:       spec.Test,
		NeedsXcode: needsXcode,
		FromSource: keep(spec.FromSource, ports),
		Requires:   edgesFor(spec, ports),
		Deactivate: deactivateFor(seated),
		// THE EVIDENCE THE ANALYSIS READS, and neither field was set by
		// anything in the tree until now.
		//
		// verify.Request.Manifest and .Baseline are documented, tart
		// honours both, run.Observe gathers what comes back, and
		// darwin/abi and internal/dependents — some three thousand lines
		// between them — decide a cohort proposal from it. Nothing asked.
		// So Observe's manifest map was always empty, abi.Delta ran with
		// Described false on every attempt, and the whole comparison
		// produced a finding about nothing.
		//
		// MEASURED ALWAYS, rather than when a caller says it wants one.
		// Request.Manifest's own doc argues for asking, on the grounds
		// that the walk costs something and a caller who only wants to
		// know whether the port builds should not pay for it — which was
		// right when the answer was read by nothing. It is the wrong trade
		// now: whether the headline HAS dependents is not knowable before
		// the build (it is a reverse-index question the settle road asks),
		// so a request that waited to be told would never be told. One
		// walk of an installed port, inside a guest that has just spent
		// ten to forty minutes building it, against a comparison that
		// otherwise cannot run at all.
		Manifest:     true,
		Baseline:     baseline,
		BaselineNote: note,
	}
	return req, runs, nil
}

// declineDetail is the sentence a declining member's record carries. It
// names the release, because a port that declines Sequoia and builds on
// Sonoma is the ordinary case and a reader must be able to tell which
// platform the refusal is about, and it quotes the port's own reason
// where the preflight read one.
func declineDetail(pf Preflight, spec Spec) string {
	what := "declares known_fail on " + spec.Platform.Name
	if pf.Reason != "" {
		what += ": " + pf.Reason
	}
	return what
}

// keep intersects a spec's from-source list with the ports actually
// being built.
//
// Intersected and not asserted over the roster: a headline that declined
// the platform at preflight is not in the guest at all, and naming it
// here would put a port in the request that no argv mentions. The
// archive is ignored where a pass earned against it would prove nothing
// — a re-derivation at an unchanged version has an archive that predates
// the change — and it is the member's own property, so a cohort ignores
// archives per member rather than wholesale.
func keep(want, built []string) []string {
	var out []string
	for _, p := range want {
		if slices.Contains(built, p) {
			out = append(out, p)
		}
	}
	return out
}

// edgesFor is the request's Requires: for each port being built, the
// members of the same request it declares a dependency on, spelled as
// the request spells them.
//
// The spec carries the graph over its ROSTER and this narrows it to what
// is being built, dropping every edge that names a member the preflight
// threw out. That is the same reason FromSource is intersected: the
// slices are parallel to Ports by contract, so a graph drawn over the
// roster would shift every index after the first decline and hand the
// provider one member's prerequisites under another member's name.
//
// A spec that carries no graph — nil, or shorter than its roster —
// declares no edges at all, which is a request whose members are
// independent of one another. That is the slow answer and never a wrong
// one: without the graph every member is attempted, and one built ahead
// of a failed prerequisite fails on its own with the prerequisite's name
// in its log, which is a reading the judge already makes.
func edgesFor(spec Spec, built []string) [][]string {
	if len(spec.Requires) == 0 || len(built) < 2 {
		return nil
	}
	at := make(map[string]int, len(spec.Roster))
	for i, m := range spec.Roster {
		at[m.Port] = i
	}
	out := make([][]string, len(built))
	any := false
	for i, p := range built {
		j, ok := at[p]
		if !ok || j >= len(spec.Requires) {
			continue
		}
		for _, dep := range spec.Requires[j] {
			if slices.Contains(built, dep) {
				out[i] = append(out[i], dep)
				any = true
			}
		}
	}
	if !any {
		return nil
	}
	return out
}

// deactivateFor is the request's Deactivate slice: parallel to the ports
// actually being built, the sibling for a forced member and "" for every
// ordinary one. Nil where nothing was forced, which is every submission
// but a person's D24 override, so the provider sees exactly the request
// it always saw.
func deactivateFor(seated []Member) []string {
	out := make([]string, len(seated))
	any := false
	for i, m := range seated {
		if m.Forced != "" {
			out[i] = m.Forced
			any = true
		}
	}
	if !any {
		return nil
	}
	return out
}
