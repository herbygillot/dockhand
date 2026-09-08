package change

import (
	"errors"

	"github.com/herbygillot/dockhand/internal/record"
)

// Act is what a hold is asked about, typed, because the two hold
// origins withhold different acts and a draft that took the act as a
// sentence ("the publication") could only answer one way for both.
type Act uint8

const (
	ActUnknown  Act = iota // rule 7: refused, never permitted
	ActVerify              // start a build: run.Pending's question
	ActPublish             // open or refresh a pull request: Authorize's
	ActDemolish            // delete the branch dockhand made: Discard's and retire's, before DemolishIn
)

// ErrHeld is the refusal every road that meets a hold returns, so a held
// branch is refused with one identity and one exit code (23) whichever
// verb met it. The sentence names the act; the caller branches on this.
var ErrHeld = errors.New("change: the change is held")

// Held is the single predicate behind every refusal that mentions a
// hold, and it answers by ORIGIN, ACT and INVOKER, which is the whole of
// the fix an adversarial pass forced:
//
//	HoldPerson    withholds every act for every invoker.
//	HoldCrossing  withholds ActPublish and ActDemolish for record.Machine
//	              only; for a person it withholds nothing — the person is
//	              warned (Crossing.Warns), never refused; and it never
//	              withholds ActVerify: run.Start performs no hold check,
//	              and run.Pending refuses only a person's hold.
//	HoldUnknown   withholds every act (rule 7): an origin nobody stamped is
//	              a wiring gap, not a permission.
//
// It lives here because a hold is a fact about the CHANGE, and it takes
// a record.Change from the state ref rather than the note's record.Record
// for the same reason: a decision does not read a derived copy. Verify
// and Accept ask it at resolve (ActVerify, Human) and refuse with exit
// 23; run.Pending asks it per queued attempt; publish.Authorize asks it
// with the invoker Facts carries; Discard's machine road asks
// ActDemolish. One predicate, one table, so the roads cannot get
// different answers for the same branch.
func Held(c record.Change, act Act, by record.Driver) error {
	if c.Hold == nil {
		return nil
	}
	if act == ActUnknown || c.Hold.Origin.WithholdsVerification() {
		return ErrHeld // an unnamed act; a person's hold; an unstamped one
	}
	if act == ActVerify {
		return nil // a crossing never withholds a build
	}
	if by == record.Machine {
		return ErrHeld
	}
	return nil
}
