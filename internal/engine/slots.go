package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

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
// live process runs counts, so two foreground checks don't run on one
// Tart host at once where providers.tart.capacity says one (the command
// UX review's §9: the capacity governed serve alone). Where it waits, the
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

// fullProviders are the providers among needs whose capacity checks live
// processes other than this one are running fill, with those checks.
func (e *Engine) fullProviders(ctx context.Context, session *coord.Session, id model.RunID, needs []string) (map[string][]model.Run, error) {
	full := map[string][]model.Run{}
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		runs, err := r.Runs(store.RunFilter{States: []model.RunState{model.RunRunning}})
		if err != nil {
			return err
		}
		holders, err := session.Holders(r, runResources(runs)...)
		if err != nil {
			return err
		}
		using := map[string][]model.Run{}
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
					using[provider] = append(using[provider], other)
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

// slotWords say what fills each full provider: "providers.tart.capacity
// is 1, and check-41 is using it".
func slotWords(full map[string][]model.Run, e *Engine) []string {
	var words []string
	for _, provider := range slices.Sorted(maps.Keys(full)) {
		var names []string
		for _, run := range full[provider] {
			names = append(names, run.Name())
		}
		words = append(words, fmt.Sprintf("providers.%s.capacity is %d, and %s %s using it", provider, e.capacity(provider), prose.And(names), map[bool]string{true: "is", false: "are"}[len(names) == 1]))
	}
	return words
}
