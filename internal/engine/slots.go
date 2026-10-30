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

// slotPoll is how often a check waiting for a provider's slot looks again.
const slotPoll = 2 * time.Second

// capacity is how many checks a provider takes at once, its
// providers.<name>.capacity (Capacity), 1 where it's unset.
func (e *Engine) capacity(provider string) int {
	return max(e.Capacity[provider], 1)
}

// awaitSlot waits, before a check runs here, until each provider it builds
// on has a slot under its capacity, as serve keeps to it: a check another
// live process runs counts, so foreground checks keep to a provider's
// capacity as serve's do (the command UX review's §9: the capacity
// governed serve alone). Where it waits, the
// run's journal says so, once.
func (e *Engine) awaitSlot(ctx context.Context, session *coord.Session, id model.RunID) error {
	run, err := e.Run(ctx, id)
	if err != nil {
		return err
	}
	needs, err := e.RunProviders(ctx, run)
	if err != nil {
		return err
	}
	said := false
	for {
		full, err := e.fullProviders(ctx, session, id, needs)
		if err != nil || len(full) == 0 {
			return err
		}
		if !said {
			said = true
			words := fmt.Sprintf("%s waits for a slot on %s: %s", run.Name(), prose.And(slices.Sorted(maps.Keys(full))), strings.Join(slotWords(full, e), "; "))
			if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
				_, err := session.Emit(tx, model.Event{Branch: run.Branch, Run: run.ID, Kind: model.EventWaiting, Level: model.LevelInfo, Message: words})
				return err
			}); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(slotPoll):
		}
	}
}

// slotHolder is a check filling a provider's capacity, and the process
// that runs it.
type slotHolder struct {
	run     model.Run
	process *model.Session
}

// fullProviders are the providers among needs whose capacity checks live
// processes other than this one are running fill, with those checks.
func (e *Engine) fullProviders(ctx context.Context, session *coord.Session, id model.RunID, needs []string) (map[string][]slotHolder, error) {
	full := map[string][]slotHolder{}
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		runs, err := r.Runs(store.RunFilter{States: []model.RunState{model.RunRunning}})
		if err != nil {
			return err
		}
		holders, err := session.Holders(r, runResources(runs)...)
		if err != nil {
			return err
		}
		using := map[string][]slotHolder{}
		for _, other := range runs {
			holder := holders[RunResource(other.ID)]
			if other.ID == id || holder == nil || holder.ID == session.ID() {
				continue
			}
			plan, err := r.Plan(other.Plan)
			if err != nil {
				return err
			}
			for _, provider := range needs {
				if slices.ContainsFunc(plan.Environments, func(environment model.Environment) bool { return environment.Provider == provider }) {
					using[provider] = append(using[provider], slotHolder{run: other, process: holder})
				}
			}
		}
		for provider, others := range using {
			if len(others) >= e.capacity(provider) {
				full[provider] = others
			}
		}
		return nil
	})
	return full, err
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

// sayHeldInServe says once, in a run's journal, that serve's queue holds
// it for a provider its own checks fill, which the person following it
// reads: a check handed to serve said nothing while it waited, where local's
// own waiting words come only once a check runs (rc1's full stage, G14). A
// local prefix another check builds in is said as local says it, "waiting
// for /opt/local, held by check-786"; any other provider as awaitSlot says
// it.
func (e *Engine) sayHeldInServe(ctx context.Context, session *coord.Session, run model.Run, full []string, inFlight map[model.RunID]model.Run) error {
	return e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		own, err := tx.Plan(run.Plan)
		if err != nil {
			return err
		}
		holders := map[string][]slotHolder{}
		var words []string
		for _, id := range slices.Sorted(maps.Keys(inFlight)) {
			other := inFlight[id]
			plan, err := tx.Plan(other.Plan)
			if err != nil {
				return err
			}
			for _, environment := range plan.Environments {
				if !slices.Contains(full, environment.Provider) || slices.ContainsFunc(holders[environment.Provider], func(h slotHolder) bool { return h.run.ID == other.ID }) {
					continue
				}
				holders[environment.Provider] = append(holders[environment.Provider], slotHolder{run: other})
				if environment.Provider == buildenv.Local && slices.ContainsFunc(own.Environments, func(mine model.Environment) bool { return mine.Prefix == environment.Prefix }) {
					words = append(words, fmt.Sprintf("waiting for %s, held by %s", environment.Prefix, other.Name()))
				}
			}
		}
		if len(words) == 0 {
			words = []string{fmt.Sprintf("waits for a slot on %s: %s", prose.And(slices.Sorted(maps.Keys(holders))), strings.Join(slotWords(holders, e), "; "))}
		}
		_, err = session.Emit(tx, model.Event{Branch: run.Branch, Run: run.ID, Kind: model.EventWaiting, Level: model.LevelInfo, Message: run.Name() + " " + strings.Join(words, "; ")})
		return err
	})
}
