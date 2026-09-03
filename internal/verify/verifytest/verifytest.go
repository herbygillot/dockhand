// Package verifytest is the in-memory Verifier the engine tests stand
// in for tart: scriptable states, recorded releases, no VM. It
// exists because everything between a submitted job and a settled note
// — settle, follow, cancel, discard — was provable only by live runs,
// and every regression in that band was being caught by a human.
package verifytest

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Fake implements verify.Verifier from plain maps a test writes.
type Fake struct {
	// Platforms is what Capabilities reports; empty defaults to one.
	Platforms []platform.Release
	// SubmitErr makes every Submit fail, for the deferred paths.
	SubmitErr error
	// States scripts Poll per job ID; a job with no entry is Running.
	States map[string]verify.Status
	// Vanished marks job IDs Poll answers ErrUnknownJob for — the
	// worker somebody deleted behind dockhand's back.
	Vanished map[string]bool
	// Logs scripts Log per job ID.
	Logs map[string]string
	// LogErr makes Log fail per job ID — the guest whose log cannot be
	// read, for the settle paths that must still record a verdict.
	LogErr map[string]error
	// ReleaseErr makes Release fail per job ID — the worker tart could
	// not delete, for the settle paths that must say so.
	ReleaseErr map[string]error
	// ExecOut scripts Exec per job ID — the fake implements
	// verify.Executor so the guest-reaching verbs are testable, but
	// deliberately not InteractiveShell: a fake terminal proves
	// nothing.
	ExecOut map[string]string
	// Live scripts Workers: every environment this provider is running,
	// accounted for or not. Named for what it holds rather than for the
	// method, because a field and a method cannot share a name.
	Live []verify.Worker
	// WorkersErr makes Workers fail — the audit the machine will not
	// answer, which a caller must absorb rather than report as an
	// empty machine.
	WorkersErr error
	// Inventory scripts Manifests per job ID. Named for what it holds
	// rather than for the method, on Live's precedent.
	Inventory map[string]verify.Manifests
	// ManifestsErr makes Manifests fail per job ID — the environment
	// that built the port and then could not describe it, which is a
	// missing comparison and never a finding about the port.
	ManifestsErr map[string]error
	// Probes scripts Probe, by job ID and then by port. The port is a
	// second key rather than part of the first because a cohort's
	// members are probed as themselves, and a test that had to spell a
	// job and a port into one string would be scripting a cache key
	// instead of an answer.
	Probes map[string]map[string][]verify.ProbeLine
	// ProbeErr makes Probe fail per job ID — the guest that will not
	// run the port's own binaries.
	ProbeErr map[string]error
	// CanManifest is what Capabilities reports for InstalledManifest: a
	// provider that can describe an installation from inside the
	// environment that made it.
	//
	// A field rather than a method because the method name Manifests is
	// taken, on Live and Inventory's precedent. It is false by default
	// deliberately, and that is not laziness: the capability is what a
	// caller reads to decide whether to ask for a manifest at all, so a
	// fake that claimed it by default would set Request.Manifest across
	// every engine test in the tree and change what those tests submit.
	//
	// Declaring it is also deliberately separate from implementing
	// Manifester. The fake implements the interface always, so a test
	// that leaves this false is scripting exactly the provider that
	// could describe an installation and does not say so — which is the
	// mismatch the caller must refuse by name rather than discover.
	CanManifest bool
	// Evidence and Xcode are what Capabilities reports for the two
	// facts a provider knows about itself. Both zero by default, so a
	// test that does not care answers exactly as this fake always has.
	Evidence string
	Xcode    map[platform.Release]bool

	// Submitted records every request, in order.
	Submitted []verify.Request
	// Released records every released job ID, in order.
	Released []string

	nextID int
}

var _ verify.Verifier = (*Fake)(nil)

func (f *Fake) Capabilities() verify.Capabilities {
	plats := f.Platforms
	if len(plats) == 0 {
		plats = []platform.Release{{Name: "Testos", Darwin: 99}}
	}
	return verify.Capabilities{
		Platforms:         plats,
		Concurrent:        2,
		Pristine:          true,
		Evidence:          f.Evidence,
		InstalledManifest: f.CanManifest,
		Xcode:             f.Xcode,
	}
}

func (f *Fake) Submit(_ context.Context, req verify.Request) (verify.Job, error) {
	if f.SubmitErr != nil {
		return verify.Job{}, f.SubmitErr
	}
	f.Submitted = append(f.Submitted, req)
	f.nextID++
	return verify.Job{Provider: "fake", ID: fmt.Sprintf("fake-%d", f.nextID), Started: time.Now()}, nil
}

func (f *Fake) Poll(_ context.Context, job verify.Job) (verify.Status, error) {
	if f.Vanished[job.ID] {
		return verify.Status{}, fmt.Errorf("%w: %s", verify.ErrUnknownJob, job.ID)
	}
	st, ok := f.States[job.ID]
	if !ok {
		return verify.Status{State: verify.Running}, nil
	}
	return st, nil
}

func (f *Fake) Log(_ context.Context, job verify.Job) (string, error) {
	if err := f.LogErr[job.ID]; err != nil {
		return "", err
	}
	return f.Logs[job.ID], nil
}

var _ verify.Executor = (*Fake)(nil)

func (f *Fake) Exec(_ context.Context, job verify.Job, _ ...string) (string, error) {
	out, ok := f.ExecOut[job.ID]
	if !ok {
		return "", fmt.Errorf("%w: %s", verify.ErrUnknownJob, job.ID)
	}
	return out, nil
}

func (f *Fake) Release(_ context.Context, job verify.Job) error {
	if err := f.ReleaseErr[job.ID]; err != nil {
		return err
	}
	f.Released = append(f.Released, job.ID)
	return nil
}

var _ verify.WorkerLister = (*Fake)(nil)

func (f *Fake) Workers(context.Context) ([]verify.Worker, error) {
	if f.WorkersErr != nil {
		return nil, f.WorkersErr
	}
	return f.Live, nil
}

var (
	_ verify.Manifester = (*Fake)(nil)
	_ verify.Prober     = (*Fake)(nil)
)

// Manifests answers what a test scripted, and the zero Manifests for a
// job it said nothing about. Nothing to compare is a state a real
// provider reports too — a build that never installed anything has no
// installed manifest — so an unscripted job must not look like a
// missing one.
func (f *Fake) Manifests(_ context.Context, job verify.Job) (verify.Manifests, error) {
	if err := f.ManifestsErr[job.ID]; err != nil {
		return verify.Manifests{}, err
	}
	return f.Inventory[job.ID], nil
}

// Probe answers the lines scripted for this job's port, and none for a
// port nothing was scripted for. A port with no probe lines is a port
// nothing was run against, which is what a provider that knows no
// probes for it reports.
func (f *Fake) Probe(_ context.Context, job verify.Job, port string) ([]verify.ProbeLine, error) {
	if err := f.ProbeErr[job.ID]; err != nil {
		return nil, err
	}
	return f.Probes[job.ID][port], nil
}

// Incapable is a Verifier and nothing else: no Executor, no
// WorkerLister, no Manifester, no Prober. It exists because a caller's
// graceful refusal for a provider that cannot answer is otherwise
// untestable — with only Fake to stand in, every optional capability is
// always present, and the branch that says so honestly never runs.
//
// It wraps a Fake rather than embedding one so the capabilities cannot
// creep back in by promotion: adding a method to Fake must not quietly
// give Incapable that method too.
type Incapable struct{ Fake *Fake }

var _ verify.Verifier = Incapable{}

func (i Incapable) Capabilities() verify.Capabilities { return i.Fake.Capabilities() }

func (i Incapable) Submit(ctx context.Context, req verify.Request) (verify.Job, error) {
	return i.Fake.Submit(ctx, req)
}

func (i Incapable) Poll(ctx context.Context, job verify.Job) (verify.Status, error) {
	return i.Fake.Poll(ctx, job)
}

func (i Incapable) Log(ctx context.Context, job verify.Job) (string, error) {
	return i.Fake.Log(ctx, job)
}

func (i Incapable) Release(ctx context.Context, job verify.Job) error {
	return i.Fake.Release(ctx, job)
}
