package branchstate

import (
	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
)

// Check is what's recorded of a branch's checks, as the verdict reads it.
type Check struct {
	// Latest is the newest finished check that isn't a baseline: of the
	// branch's files as they are now where Current, else of others.
	Latest  *model.Run
	Current bool
	// Active are the runs queued or running, the branch's check and any
	// baseline beside it, oldest first.
	Active []model.Run
	// Stopped is a check whose process ended before it finished.
	Stopped *model.Run
	// Evidence is what the checks of the files as they are establish.
	Evidence *evidence.Evidence
	// Accepted are the ports whose failures a person accepted at this
	// commit, AtBase the failed ports a baseline fails alike, with the
	// baselines, and Decisions the standing ones that may take a
	// dependency's failure: what publication reads beside the evidence
	// (Publishable).
	Accepted  []string
	AtBase    map[string][]string
	Decisions []model.Decision
}

// Publishable reports whether the files' checks let them be published:
// passed, or failed with no publication block left once the decisions
// that stand for this evidence are read, a person's at this commit or a
// standing one (PublicationBlocks). The run's outcome stays failed;
// acceptance changes whether the work goes on, not what happened. It's
// the one judgment submit, serve's candidates, ship --passing, attention,
// and Next: read, where submit took a standing acceptance and the rest
// didn't (Codex's review of 8c007979, C8-3).
func (c Check) Publishable() bool {
	switch c.Standing() {
	case CheckPassed:
		return c.Evidence != nil
	case CheckFailed:
		return c.Evidence != nil && len(PublicationBlocks(*c.Evidence, c.Accepted, c.AtBase, StandingAccepted(*c.Evidence, c.Decisions))) == 0
	}
	return false
}

// Standing is what a branch's checks establish of its files as they are
// now: the one verdict status words, serve's passing branches read, and
// submit's blocking reasons spell out target by target (the command
// package review's concept 2, where three places judged it apart). It's
// judged against build.default's environments, which the evidence
// requires.
type Standing int

const (
	// NotChecked is a branch no finished check has looked at.
	NotChecked Standing = iota
	// Checking is a check queued or running.
	Checking
	// CheckStopped is a check whose process ended before it finished.
	CheckStopped
	// CheckPassed is the latest check's pass of the files as they are
	// now, every result it needs standing.
	CheckPassed
	// CheckIncomplete is a pass that no longer stands everywhere it's
	// required, as after an environment changed, or one never built.
	CheckIncomplete
	// CheckedOlder is a pass of other files than these.
	CheckedOlder
	// CheckFailed is a check that failed.
	CheckFailed
	// CheckAttention is a check that ended needing a person's look.
	CheckAttention
)

// String is the standing's name, as status's JSON gives it: not-checked,
// checking, stopped, passed, incomplete, older, failed, or attention.
func (s Standing) String() string {
	return [...]string{"not-checked", "checking", "stopped", "passed", "incomplete", "older", "failed", "attention"}[s]
}

// Checking is the branch's one check queued or running (Run.BranchCheck),
// nil while there's none, whatever baseline or stopping run is active: a
// baseline of a passed check left the branch Checking, and serve waiting
// on it, where ActiveCheck said there was none.
func (c Check) Checking() *model.Run {
	for i := len(c.Active) - 1; i >= 0; i-- {
		if c.Active[i].BranchCheck() {
			return &c.Active[i]
		}
	}
	return nil
}

// Standing judges the branch's checks.
func (c Check) Standing() Standing {
	switch {
	case c.Stopped != nil:
		return CheckStopped
	case c.Checking() != nil:
		return Checking
	case c.Latest == nil:
		return NotChecked
	}
	switch c.Latest.State {
	case model.RunPassed:
		switch {
		case c.Evidence != nil && len(c.Evidence.Failed()) > 0:
			return CheckIncomplete
		case !c.Current:
			return CheckedOlder
		}
		return CheckPassed
	case model.RunFailed:
		return CheckFailed
	}
	return CheckAttention
}
