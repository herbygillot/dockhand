package tart

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/verify"
)

// The capability is the contract, provably.
var _ verify.VacancyReporter = Provider{}

// Vacancy reports how much room this machine has right now: the same
// live count Admit gates on, against the same ceiling, stamped with the
// moment it was taken.
//
// IT REUSES THE MACHINE-WIDE RUNNING COUNT AND NOT Workers(), which is
// the trap verify.VacancyReporter's doc names. Workers() lists guests by
// the dockhand-worker- prefix and does not filter by state, so a free
// count derived from it would miss a person's own `tart run` (which
// spends an Apple licence slot just the same) and would count dockhand's
// own stopped guests (which spend nothing). `tart list` is the truth
// here for the same reason it is the truth for admission.
//
// IT IS AN OBSERVATION AND NEVER A PERMISSION. Admit is the authority:
// it counts again under the machine-wide lock, and a right-sized batch
// can still meet ErrNoVacancy mid-pass because a person's `tart run`
// takes no lock at all. This buys the drain an economy — not starting
// forty attempts to seat two — and it is what `status` shows.
//
// IT NEVER RETURNS ErrNoVacancy. A full machine is Known with Free zero,
// which is a legitimate answer; letting the observer refuse would make
// the sentinel mean both "full" and "could not ask".
//
// Known is derived from the listing SUCCEEDING AND PARSING, and the two
// failures are told apart because their recoveries differ. A listing
// that failed is an error — the machine could not be asked, and the
// caller (app.Cycle.ask) turns that into the unknown Vacancy for the
// report. Output that came back and could not be read is the case rule 7
// is really about: countRunning yields a bare 0 for anything it does not
// recognise, and 0 running on a two-slot machine reads as ALL THE ROOM
// IN THE WORLD. So an unreadable listing is Known false with the ceiling
// still stated, and Vacancy.Admits then admits nothing — the safe
// direction for a caller that asked, and one no starter reads as a
// reason to stop.
func (p Provider) Vacancy(ctx context.Context) (verify.Vacancy, error) {
	out, err := listVMs(ctx, p.Tools)
	if err != nil {
		return verify.Vacancy{}, wrapListing(out, err)
	}
	busy, ok := tallyRunning(out)
	if !ok {
		return verify.Vacancy{Limit: concurrent, AsOf: observedAt()}, nil
	}
	free := concurrent - busy
	if free < 0 {
		// More guests than Apple's limit, because a person started some by
		// hand. Free is what could be seated, and that is none of them.
		free = 0
	}
	return verify.Vacancy{Known: true, Free: free, Limit: concurrent, AsOf: observedAt()}, nil
}

// observedAt is the clock a vacancy is stamped from — a variable so a
// test can pin AsOf rather than assert about the second it ran in, and
// named for the field it fills rather than `now`, because a bare `now`
// at package scope in a package this size is a name every function has
// to avoid for a local.
var observedAt = time.Now

// wrapListing gives a failed `tart list` the fact it carries: the
// machine could not be asked, which is verify.ErrNoEnvironment, in the
// same words runningVMs uses for the same failure.
func wrapListing(out string, err error) error {
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("%w: listing VMs for vacancy: %w", verify.ErrNoEnvironment, err)
	}
	return fmt.Errorf("%w: listing VMs for vacancy: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
}

// tartStates is the vocabulary `tart list` prints in its last column,
// and it is here so that a column this reader does not understand is a
// fact it declines to report rather than a guest it silently decides is
// idle. A tart that grew a sixth state would make this answer unknown,
// which is the honest reading: dockhand cannot say whether that guest is
// spending a slot.
var tartStates = map[string]bool{"running": true, "stopped": true, "suspended": true}

// tallyRunning counts the running VMs in a `tart list` listing and says
// whether it could read the listing at all.
//
// The shape it requires is the one tart prints: a header row whose last
// column is `State`, then one row per VM whose last column is a state
// this reader knows. Anything else — an empty answer, a tart that
// printed a diagnostic on stdout, a future format — is not a listing
// with no running VMs in it, and the difference is the whole reason this
// function returns two values where countRunning returns one.
//
// countRunning keeps its single return because ADMISSION is on the other
// side of the same coin: an unreadable listing there yields 0 busy and
// Admit lets the caller through to `tart run`, where tart's own refusal
// and Apple's ceiling are the backstop. Here there is no backstop — a
// scheduler that believed a fabricated 0 would start every queued
// attempt on the machine at once — so this road refuses to guess and the
// other does not.
func tallyRunning(out string) (int, bool) {
	n := 0
	header := false
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		last := fields[len(fields)-1]
		if !header {
			if len(fields) < 2 || last != "State" {
				return 0, false
			}
			header = true
			continue
		}
		if len(fields) < 2 || !tartStates[last] {
			return 0, false
		}
		if last == "running" {
			n++
		}
	}
	return n, header
}
