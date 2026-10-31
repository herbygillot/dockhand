package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
)

// slotPoll is how often a check waiting for a provider's slot looks again:
// two of the engine's polls, 2 seconds unless a test sets them.
func (e *Engine) slotPoll() time.Duration { return 2 * e.pollInterval() }

// capacity is how many checks a provider takes at once, its
// providers.<name>.capacity (Capacity), 1 where it's unset.
func (e *Engine) capacity(provider string) int {
	return max(e.Capacity[provider], 1)
}

// awaitSlot waits, before a check runs here, until reserve takes its
// slots, and returns the run's lease, which its driver holds: foreground
// checks keep to a provider's capacity as serve's do (the command UX
// review's §9: the capacity governed serve alone). Where it waits, the
// run's journal says so, once, with what fills each provider. A live
// process that took the run meanwhile is a coord.HeldError.
func (e *Engine) awaitSlot(ctx context.Context, session *coord.Session, id model.RunID) (model.Lease, error) {
	run, err := e.Run(ctx, id)
	if err != nil {
		return model.Lease{}, err
	}
	needs, err := e.RunProviders(ctx, run)
	if err != nil {
		return model.Lease{}, err
	}
	said := false
	for {
		lease, full, err := e.reserve(ctx, session, run, needs)
		if err != nil || len(full) == 0 {
			return lease, err
		}
		if !said {
			said = true
			if err := e.sayHeld(ctx, session, run, full); err != nil {
				return model.Lease{}, err
			}
		}
		select {
		case <-ctx.Done():
			return model.Lease{}, ctx.Err()
		case <-time.After(e.slotPoll()):
		}
	}
}

// reserve takes a run's slots, the lease its driver holds, where each
// provider its plan builds on has fewer checks than its capacity held by
// live processes, serve's and this one's among them, deciding and taking
// in one transaction: the one capacity every driver keeps to, where a
// batch reserved its own slots, serve counted its own runs, and a
// foreground check counted other processes' alone, so a batch's checks
// didn't count serve's, nor serve's a batch's (Codex's review of
// 275125ff, finding 4). Where one is full, nothing is taken, and full
// says what fills each.
func (e *Engine) reserve(ctx context.Context, session *coord.Session, run model.Run, needs []string) (model.Lease, map[string][]slotHolder, error) {
	var lease model.Lease
	var full map[string][]slotHolder
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		var err error
		if full, err = e.fullProviders(tx, session, run.ID, needs); err != nil || len(full) > 0 {
			return err
		}
		lease, err = session.AcquireIn(tx, RunResource(run.ID))
		return err
	})
	return lease, full, err
}

// slotHolder is a check filling a provider's capacity, and the process
// that runs it.
type slotHolder struct {
	run     model.Run
	process *model.Session
}

// fullProviders are the providers among needs whose capacity the checks
// live processes hold fill, queued or running, with those checks; the run
// itself doesn't count.
func (e *Engine) fullProviders(r store.Reader, session *coord.Session, id model.RunID, needs []string) (map[string][]slotHolder, error) {
	runs, err := r.Runs(store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}})
	if err != nil {
		return nil, err
	}
	holders, err := session.Holders(r, runResources(runs)...)
	if err != nil {
		return nil, err
	}
	using := map[string][]slotHolder{}
	for _, other := range runs {
		holder := holders[RunResource(other.ID)]
		if other.ID == id || holder == nil {
			continue
		}
		plan, err := r.Plan(other.Plan)
		if err != nil {
			return nil, err
		}
		for _, provider := range needs {
			if slices.ContainsFunc(plan.Environments, func(environment model.Environment) bool { return environment.Provider == provider }) {
				using[provider] = append(using[provider], slotHolder{run: other, process: holder})
			}
		}
	}
	full := map[string][]slotHolder{}
	for provider, others := range using {
		if len(others) >= e.capacity(provider) {
			full[provider] = others
		}
	}
	return full, nil
}

// slotWords say what fills each full provider, with the process that
// runs each check, its build, and how long the check has run, as status
// -v's processes give them: "providers.command.capacity is 1, and
// check-616 is using it (a foreground dockhand, pid 4242, v0.5.0-rc5, for
// 2 hours)"; Tart's is the Mac's VMs, which no setting names.
// "check-616 is using it" said nothing of whose it was (the bazel and
// exiv2 sessions' notes, 2026-10-24).
func slotWords(full map[string][]slotHolder, e *Engine) []string {
	var words []string
	for _, provider := range slices.Sorted(maps.Keys(full)) {
		var names []string
		for _, held := range full[provider] {
			name := held.run.Name()
			if p := held.process; p != nil {
				ran := "under a minute"
				if age := e.now().Sub(held.run.CreatedAt); age >= time.Minute {
					ran = AgeWords(age.Round(time.Minute))
				}
				name += fmt.Sprintf(" (a %s dockhand, pid %d, %s, for %s)", p.Kind, p.PID, p.Version, ran)
			}
			names = append(names, name)
		}
		limit, it := fmt.Sprintf("providers.%s.capacity is %d", provider, e.capacity(provider)), "it"
		if provider == buildenv.Tart {
			limit, it = fmt.Sprintf("Tart takes as many checks as the Mac runs VMs, %d", e.capacity(provider)), "them"
		}
		words = append(words, fmt.Sprintf("%s, and %s %s using %s", limit, prose.And(names), map[bool]string{true: "is", false: "are"}[len(names) == 1], it))
	}
	return words
}

// sayHeld says, in a run's journal, what holds the slots it waits for,
// which the person following it reads, wherever it waits: a check handed
// to serve said nothing while it waited, where local's own waiting words
// come only once a check runs (rc1's full stage, G14). A local prefix
// another check builds in is said as local says it, "waiting for
// /opt/local, held by check-786"; any other provider as "check-820 waits
// for a slot on tart: …", with each holder's process.
func (e *Engine) sayHeld(ctx context.Context, session *coord.Session, run model.Run, full map[string][]slotHolder) error {
	return e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		own, err := tx.Plan(run.Plan)
		if err != nil {
			return err
		}
		var words []string
		for _, held := range full[buildenv.Local] {
			plan, err := tx.Plan(held.run.Plan)
			if err != nil {
				return err
			}
			for _, environment := range plan.Environments {
				if environment.Provider == buildenv.Local && slices.ContainsFunc(own.Environments, func(mine model.Environment) bool { return mine.Prefix == environment.Prefix }) {
					words = append(words, fmt.Sprintf("waiting for %s, held by %s", environment.Prefix, held.run.Name()))
				}
			}
		}
		if len(words) == 0 {
			words = []string{fmt.Sprintf("waits for a slot on %s: %s", prose.And(slices.Sorted(maps.Keys(full))), strings.Join(slotWords(full, e), "; "))}
		}
		_, err = session.Emit(tx, model.Event{Branch: run.Branch, Run: run.ID, Kind: model.EventWaiting, Level: model.LevelInfo, Message: run.Name() + " " + strings.Join(words, "; ")})
		return err
	})
}
