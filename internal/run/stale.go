package run

import (
	"slices"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Staleness is what the stale stage found: the attempts on a former tip
// that are still Active (to be Finished with Interrupt Superseded) and
// the leases still Held by finished old-tip attempts (kept debug
// environments, --keep-env passes: released with the verdict standing).
type Staleness struct {
	Active []record.Attempt
	Kept   []record.Lease
}

// Stale names what a change's former tips still hold, and it is PURE
// over one read. The predicate is one comparison — an attempt of this
// change whose Sha is not the tip — because attempts are keyed by
// ChangeID in the store. The shipped SupersedeStale needed ancestry OR
// the branch's reflog to find them, because notes are keyed by sha and
// "the commonest way past a failure is an amend, which ancestry cannot
// see"; keying attempts by identity makes both lookups unnecessary, and
// the reflog dependency with them. Verify, Accept, Cancel and Discard
// all run this first, then Finish(Interrupt Superseded) per Active row
// and lease.Release per Kept row — the field defect SupersedeStale was
// written for (a superseded build pinning a slot for hours), as a stage
// every road shares rather than a pass one verb remembered to run.
//
// A KEPT LEASE IS ONE THIS CHANGE'S OWN FINISHED ATTEMPTS LEFT HELD, and
// the join is through those attempts rather than through the change's
// id alone. A lease is keyed by (change, platform) and a change that is
// verified, kept and re-verified holds one lease for the LIVE attempt
// too — reporting that one as stale would hand back the environment of
// the build the caller just started. So a lease is Kept here only when
// no attempt of this change at the CURRENT tip is still using it.
//
// The rows come back in a stable order — attempts and leases by their
// own ids — so two passes over one state supersede the same things in
// the same order and a report can be pinned.
func Stale(s statestore.State, c record.Change, tip string) Staleness {
	var out Staleness
	live := map[string]bool{}
	for _, id := range sortedAttempts(s) {
		a := s.Attempts[id]
		if a.Change != c.ID {
			continue
		}
		if a.Sha == tip {
			// The current tip's own work, whatever phase it is in. Its
			// lease is not stale and its build is not superseded.
			if a.Lease != "" {
				live[a.Lease] = true
			}
			continue
		}
		if a.Active() {
			out.Active = append(out.Active, a)
			continue
		}
		if a.Settled() && a.Lease != "" {
			if l, ok := s.Leases[a.Lease]; ok && l.Held() {
				out.Kept = append(out.Kept, l)
			}
		}
	}
	out.Kept = slices.DeleteFunc(out.Kept, func(l record.Lease) bool { return live[l.Request] })
	return out
}
