package verify

import (
	"context"
	"errors"
)

// Holding is one resource a provider is keeping on dockhand's behalf:
// a guest, a scratch clone, an image. It is what a HOUSEKEEPING caller
// sees, and it is deliberately not verify.Worker.
//
// Worker answers the AUDIT's question — which environments are running
// right now, and which lease does each of them join — so it carries a
// request id and an attribution and nothing else, because that is all a
// join needs. A purge asks a different question: what is on this
// machine that dockhand made, and which of it may go. The two
// populations are not the same one seen twice. A base image is a
// holding and never a worker; a running worker is both, and it is the
// KIND that tells a caller which questions it may ask of this value.
//
// Job is set for a holding the provider can also name as a job — a
// worker — and is the zero Job for one it cannot. Nothing here decides
// from it: Discard takes the Holding, so a caller never has to know
// whether this backend's images are addressable as jobs. It rides along
// so that a caller holding a Holding can reach Poll or Log without a
// second listing.
type Holding struct {
	Name string
	Kind HoldingKind
	Job  Job
}

// HoldingKind is what a resource IS to the provider that made it, and
// it exists so that "which of these may a purge remove" is answered
// once, here, rather than by every caller pattern-matching a naming
// scheme it should not know. Reading a base image out of the string
// "dockhand-base-sequoia" is exactly the reach-past-the-kernel that
// WorkerLister was introduced to end.
//
// The provider CLASSIFIES and the caller DECIDES. That split is the
// whole design of this type: a backend knows which of its resources is
// a reference copy and which is rebuilt from one, and it knows nothing
// about whether this invocation wants them gone.
type HoldingKind uint8

const (
	// HoldingUnknown is the refusing zero, and it is the reason this is
	// an enum rather than a bool. A holding nobody classified is not a
	// removable one: a value built and not populated, a backend that
	// grew a fifth kind and a switch that fell through, and Removable
	// answers false — so the failure mode of every mistake here is a
	// resource that survives a purge, not one that is destroyed by a
	// default. Rule 7, at the one boundary in this package where the act
	// on the other side is irreversible.
	HoldingUnknown HoldingKind = iota
	// HeldWorker is an environment created for one verification. It is
	// the population WorkerLister reports, seen through this type.
	HeldWorker
	// HeldScratch is a short-lived guest no verdict rests on — a probe
	// clone, a throwaway boot. It owes nothing to anybody and it is
	// always safe to remove; one that is still here is one a crash
	// stranded.
	HeldScratch
	// HeldDerived is a resource the provider can make again from a
	// HeldReference one: a prepared base image, restorable by cloning.
	// Removing it costs the clone and nothing else, which is what makes
	// it a purge's business at all.
	HeldDerived
	// HeldReference is the copy a derived one is rebuilt FROM. It is the
	// one thing on the machine that cannot be reconstructed locally —
	// remaking it means fetching and provisioning from scratch — so a
	// purge keeps it, and Discard refuses it at the provider as well.
	// Two refusals for one rule, because the caller's is a policy and
	// the provider's is a fact about what it can put back.
	HeldReference
)

// Removable reports a holding a purge may take: one the provider can
// produce again without leaving this machine.
//
// A METHOD SO THAT A FIFTH KIND IS A COMPILE-TIME VISIT HERE rather
// than a missed case at each call site, which is Outcome.Closes' and
// Standing.Seizable's shape and their reason. It is stated once and
// read everywhere, and the unknown zero falls out of it as false.
func (k HoldingKind) Removable() bool {
	return k == HeldWorker || k == HeldScratch || k == HeldDerived
}

// String is what a report prints. A kind nobody set says so rather than
// printing a number, because the line a person reads after a purge is
// the only account they get of what was on the machine.
//
// THE SWITCH NAMES THE ZERO rather than letting it fall to the return
// below, which is Removable's rule said a second way: a fifth kind is a
// compile-time visit here, and the trailing return is then genuinely
// unreachable for every value this package declares — it is there for a
// number cast in from outside the type's own vocabulary.
func (k HoldingKind) String() string {
	switch k {
	case HoldingUnknown:
		return "unclassified"
	case HeldWorker:
		return "worker"
	case HeldScratch:
		return "scratch"
	case HeldDerived:
		return "image"
	case HeldReference:
		return "reference image"
	}
	return "unclassified"
}

// ErrKept is a provider refusing to discard a holding it will not
// remake: the second half of Removable's rule, said by the party that
// knows. A caller meeting it has asked for something the machine cannot
// put back, and the honest response is to report the name rather than
// to try another verb.
var ErrKept = errors.New("verify: this holding is a reference copy and is not removed")

// Keeper is the optional capability of naming everything the provider
// holds on dockhand's behalf, and removing one of them.
//
// It is separate from WorkerLister rather than a widening of it, and
// the reason is the same one that keeps Manifester separate from
// Executor: the two answer different questions and their callers are
// disjoint. lease.Outstanding wants running environments joined to
// leases and must not be handed images; a purge wants everything and
// must not have to infer the rest from a worker listing. A backend can
// honestly implement one and not the other — a hosted CI provider lists
// jobs and keeps no images at all.
//
// Discard takes a Holding rather than a name so the provider can refuse
// a HeldReference by kind without parsing its own naming scheme back
// out of a string, and so no caller has to learn whether this backend's
// images are addressable as jobs. A holding that is already gone is not
// an error: the provider answers ErrUnknownJob, which is the same word
// every other verb here uses for a resource it does not have, and a
// listing that straddled somebody else's deletion is the ordinary case.
//
// A caller that needs this type-asserts, and one meeting a provider
// without it has learned NOTHING about that provider's resources rather
// than learning there are none. That distinction is the whole of rule 7
// at this boundary and estate.Survey is where it is enforced.
type Keeper interface {
	Holdings(ctx context.Context) ([]Holding, error)
	Discard(ctx context.Context, h Holding) error
}
