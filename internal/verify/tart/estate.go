package tart

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/verify"
)

// Holdings implements verify.Keeper: every VM on this machine that
// dockhand named, classified by the role its name says it has.
//
// THIS IS THE ONE PLACE THE NAMING SCHEME IS READ BACKWARDS, and that
// is the whole reason the capability exists. Before it, a caller that
// wanted to clear this machine had to know that a worker starts with
// "dockhand-worker-" and a golden with "dockhand-golden-"; the kernel
// would have learned one backend's spelling, which is the defect
// WorkerLister was introduced to remove and the one a purge reaching
// past it would have reintroduced at a destructive boundary.
//
// PREFIX, NOT SUBSTRING, on workerNames' own precedent and for its
// reason: dockhand-base-sonoma must not be found inside
// dockhand-base-sonoma-anything, and a golden must never read as a
// base. Each name is matched against the four prefixes in a fixed
// order, and the FIRST match wins — the prefixes are disjoint, so the
// order is a statement about determinism and not about precedence.
//
// A VM THIS PROVIDER DID NOT NAME IS NOT REPORTED AT ALL. The listing
// is the whole machine's, including a person's own unrelated guests,
// and a holding is by definition something dockhand made: reporting a
// stranger's VM here would put it in front of a sweep whose policy is
// "remove what is removable". Omission is the refusal, and it is the
// safe direction (rule 7).
func (p Provider) Holdings(ctx context.Context) ([]verify.Holding, error) {
	out, err := listQuiet(ctx, p.Tools)
	if err != nil {
		// Both halves, because they carry different failures: tart's own
		// diagnostics land in the transcript, and a tart that is not on
		// the machine at all produces no transcript and an error that
		// already says so. This is Workers' shape, on the wider listing.
		if detail := strings.TrimSpace(out); detail != "" {
			return nil, fmt.Errorf("%w: listing this machine's VMs: %s", verify.ErrNoEnvironment, detail)
		}
		return nil, fmt.Errorf("%w: listing this machine's VMs: %w", verify.ErrNoEnvironment, err)
	}
	var held []verify.Holding
	for _, line := range strings.Split(out, "\n") {
		vm := strings.TrimSpace(line)
		kind, ours := holdingKind(vm)
		if !ours {
			continue
		}
		h := verify.Holding{Name: vm, Kind: kind}
		if kind == verify.HeldWorker {
			// That a job's id IS the VM's name is this provider's own fact,
			// stated in Workers and repeated here for the one kind a caller
			// may also poll, log or release. The other three are not jobs
			// and carry the zero Job rather than a job id that would name a
			// verification nobody asked for.
			h.Job = verify.Job{Provider: "tart", ID: vm, Request: RequestOf(vm)}
		}
		held = append(held, h)
	}
	return held, nil
}

// holdingKind reads a VM's role out of its name, and reports whether
// dockhand named it at all.
//
// The bool is separate from the kind rather than folded into
// HoldingUnknown, because the two are different answers to different
// questions: "dockhand did not make this" is not "dockhand made this
// and could not classify it". The first is omitted from the listing
// entirely; the second, if this provider ever produces one, is reported
// and kept.
func holdingKind(vm string) (verify.HoldingKind, bool) {
	switch {
	case vm == "":
		return verify.HoldingUnknown, false
	case strings.HasPrefix(vm, WorkerPrefix):
		return verify.HeldWorker, true
	case strings.HasPrefix(vm, ProbePrefix):
		return verify.HeldScratch, true
	case strings.HasPrefix(vm, GoldenPrefix):
		// BEFORE the base test and not after it, though the prefixes are
		// disjoint and the order cannot matter today. GoldenName's own doc
		// records why the two names were made disjoint in the first place
		// — a golden named dockhand-base-sequoia-golden would contain the
		// base's name, and everything matching a base by prefix would find
		// two — and putting the copy that must SURVIVE first is the shape
		// that stays correct if anyone ever undoes that.
		return verify.HeldReference, true
	case strings.HasPrefix(vm, BasePrefix):
		return verify.HeldDerived, true
	}
	return verify.HoldingUnknown, false
}

// Discard implements verify.Keeper: it removes one holding, and it
// refuses a reference copy.
//
// THE REFUSAL IS THE PROVIDER'S OWN AND IT DUPLICATES NOTHING. The
// caller's policy (estate.Sweep, over HoldingKind.Removable) is about
// what a purge wants; this one is about what this machine can put back.
// A golden is the only image here that cannot be rebuilt without
// leaving the machine — restoring a base means cloning a golden, which
// under copy-on-write costs neither time nor disk, and restoring a
// golden means fetching and provisioning from scratch — so the backend
// that knows that says so, and a caller that asked anyway is told which
// name it asked about.
//
// An absent VM is verify.ErrUnknownJob, which is the word every other
// verb here uses and which estate.Sweep reads as done: a holding a
// listing named and someone else removed in between is the ordinary
// case, not a fault.
func (p Provider) Discard(ctx context.Context, h verify.Holding) error {
	if h.Name == "" {
		return fmt.Errorf("%w: a holding with no name", verify.ErrUnknownJob)
	}
	if !h.Kind.Removable() {
		return fmt.Errorf("%w: %s", verify.ErrKept, h.Name)
	}
	if ok, err := HasVM(ctx, p.Tools, h.Name); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %s", verify.ErrUnknownJob, h.Name)
	}
	// The sidecar goes with a worker and there is nothing to clear for
	// anything else: clearAttribution is keyed by the VM's name and is a
	// no-op for a name that never had one.
	defer clearAttribution(h.Name)
	return removeVM(ctx, p.Tools, h.Name)
}

// The capability is the contract, provably.
var _ verify.Keeper = Provider{}
