package run

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/internal/artifact"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Observe polls one job and gathers its evidence: the status, the log,
// the member states, and — while the lease is still held — the GUEST
// half of a cohort proposal, each member's manifests and probes. No
// decisions. It is called by Finish and by nothing else: R10 says one
// judge per job, and the judge is whoever runs Finish, chosen by
// residency. It takes no index and no tree: the local half is the
// propose step's, so this function's I/O is the provider's and only the
// provider's.
//
// THE ORDER IS THE WHOLE OF IT. The poll comes first because everything
// after it is conditional on what it said; the log is fetched before any
// release, because handing a guest back puts its log out of reach; the
// guest's own member record is asked only of a failed cohort, because
// that is the one shape in which the members' verdicts part; and the
// manifests and probes are taken last and only from a job that has
// FINISHED — a comparison against a half-finished install would measure
// the change against itself, and an ABI reading of a build still in
// flight is the strongest false break there is.
//
// NOTHING HERE FAILS THE SETTLEMENT except a poll that could not be made
// at all. An environment that built the port and then could not describe
// it is a missing observation and never a verdict about the port, so the
// gatherings below absorb their own refusals and the propose step says
// the check was unavailable. A Manifester failure that travelled would
// strand every member of that guest as running forever, which is the
// opposite of what the evidence is for.
//
// A JOB THE PROVIDER HAS LOST is Vanished and not an error: that is what
// a worker deleted out from under a record looks like, and Judge has a
// reading for it.
//
// IT TAKES THE LEASE WHERE THE SKETCH TOOK THE ATTEMPT, and the swap is
// forced rather than preferred. record.Attempt.Lease is the REQUEST
// TOKEN — the name the store keys the lease document under, minted
// before the provider was ever called — while a verify.Job is addressed
// by Provider and ID, which only the lease carries (record.LeaseID, the
// adapter that exists so the durable shape is not hostage to the
// provider interface). Polling by the token would poll a job no provider
// has. The attempt itself is then unread here: everything this function
// needs about the question is on the Spec, and everything it needs about
// the environment is on the lease. Finish reads both in one read.
func Observe(ctx context.Context, prov verify.Verifier, l record.Lease, spec Spec, prior map[string]record.Run) (Evidence, error) {
	caps := prov.Capabilities()
	e := Evidence{
		Lease:    l.ID,
		Platform: spec.Platform,
		Spec:     spec,
		Prior:    prior,
		Claim:    caps.Evidence,
		At:       time.Now().UTC(),
	}
	job := jobOf(l)
	st, err := prov.Poll(ctx, job)
	switch {
	case errors.Is(err, verify.ErrUnknownJob):
		e.Vanished = true
		return e, nil
	case err != nil:
		// A provider that cannot answer settles nothing at all. Returning
		// the failure rather than an empty Evidence is what stops a
		// half-settled record: the caller writes nothing, and the next pass
		// observes again.
		return Evidence{}, err
	}
	e.Status = st
	if !st.State.Terminal() {
		return e, nil
	}
	if needsLog(st.State) {
		if log, lerr := prov.Log(ctx, job); lerr == nil {
			e.Log, e.LogRead = log, true
		}
	}
	// The guest's own record of each member, beside the log: it is what
	// tells a member skipped for a failed prerequisite from one the runner
	// never reached, and the judge trusts it (D25). Asked only of a failed
	// cohort, because that is the one shape in which the members' verdicts
	// part — a passing guest passed every member it announced, and one
	// subject has no record. A provider that cannot answer, or a guest
	// whose record cannot be read, leaves the log to speak alone.
	if st.State == verify.Failed && len(spec.Roster) > 1 {
		if ms, ok := prov.(verify.MemberStater); ok {
			if states, serr := ms.MemberStates(ctx, job); serr == nil {
				e.Members = states
			}
		}
	}
	var missed string
	e.Manifests, missed = manifestsOf(ctx, prov, job, spec)
	if missed != "" {
		e.Unavailable = append(e.Unavailable, missed)
	}
	e.Probes = probesOf(ctx, prov, job, spec, prior)
	return e, nil
}

// manifestsOf asks the environment what it installed, while it is still
// holding it.
//
// Both capabilities are asked for separately and by name. A provider
// that DECLARES InstalledManifest and implements no Manifester is
// answered with nothing rather than with an empty comparison, because an
// empty comparison reads as every library removed — the strongest false
// break there is — and the declaration is what the submission read to
// ask for a manifest at all. The two gates are deliberately apart: a
// provider reconfigured between them produces a request nobody can
// answer, and a caller that checked only one would report that as an ABI
// result rather than as a check that was unavailable.
//
// THE ANSWER IS ONE PORT'S, AND THAT IS THE CONTRACT'S OWN LIMIT rather
// than a simplification here. verify.Manifester answers for a JOB —
// Manifests(ctx, job) — so what comes back is the headline's two sides,
// and the shipped settlement keyed it under the headline for exactly
// this reason. The map is per member because the evidence is per member
// wherever a provider can say so, and today exactly one entry is
// written: the headline's. A cohort's dependents are measured against
// the headline's ABI, so the entry the proposal needs is the one that
// exists.
// It reports what it could not get beside what it got. A provider that
// declares no manifest capability is answered by abi's own "this
// environment cannot describe an installation", which is true; a
// provider that CAN describe and was asked and failed is a different
// fact wearing the same empty map, and saying nothing about it made a
// broken analysis path look like a clean one (rule 7, stacked).
func manifestsOf(ctx context.Context, prov verify.Verifier, job verify.Job, spec Spec) (map[string]Manifests, string) {
	if len(spec.Roster) == 0 || !prov.Capabilities().InstalledManifest {
		return nil, ""
	}
	m, ok := prov.(verify.Manifester)
	if !ok {
		return nil, ""
	}
	got, err := m.Manifests(ctx, job)
	if errors.Is(err, verify.ErrUnknownJob) {
		// The guest is gone, so there is nothing to describe. Not an
		// unobtained check: nothing could have obtained it.
		return nil, ""
	}
	if err != nil {
		return nil, "the environment was asked what it installed and could not answer: " + err.Error()
	}
	return map[string]Manifests{
		spec.Roster[0].Port: {
			Baseline:  got.Baseline,
			Candidate: got.Installed,
			Source:    got.BaselineSource,
			Reason:    got.BaselineReason,
		},
	}, ""
}

// probesOf runs each member's own binaries in the environment and keeps
// what they said — the cheapest evidence that a build which succeeded
// also produced something that runs.
//
// Per member, because a job may have built several and each is probed as
// itself. A member that already reached a terminal verdict is skipped:
// it is in the roster so the log's blame can find it, and this pass is
// not writing its run.
func probesOf(ctx context.Context, prov verify.Verifier, job verify.Job, spec Spec, prior map[string]record.Run) map[string][]artifact.Probe {
	p, ok := prov.(verify.Prober)
	if !ok {
		return nil
	}
	out := map[string][]artifact.Probe{}
	for _, m := range spec.Roster {
		if !judgeable(prior[m.Port]) {
			continue
		}
		lines, err := p.Probe(ctx, job, m.Port)
		if err != nil {
			continue
		}
		out[m.Port] = lines
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
