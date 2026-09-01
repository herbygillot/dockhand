// Package verifytest is the in-memory Verifier the lifecycle tests
// stand in for tart: scriptable states, recorded releases, no VM. It
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
	// ExecOut scripts Exec per job ID — the fake implements
	// verify.Executor so the guest-reaching verbs are testable, but
	// deliberately not InteractiveShell: a fake terminal proves
	// nothing.
	ExecOut map[string]string

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
	return verify.Capabilities{Platforms: plats, Concurrent: 2, Pristine: true}
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
	f.Released = append(f.Released, job.ID)
	return nil
}
