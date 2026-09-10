package planning

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
)

// oracle is the scripted evaluator the seam exists for: a plan is
// produced here with no MacPorts installation, which is the property the
// package doc claims and this file proves.
type oracle struct{ vals info.Values }

func (o oracle) Values(context.Context, string, string, info.VariantSet) (info.Values, error) {
	return o.vals, nil
}
func (o oracle) Snapshot(context.Context, string, info.VariantSet) (info.Snapshot, error) {
	return info.Snapshot{}, nil
}
func (o oracle) Subports(context.Context, string) ([]string, error) { return nil, nil }
func (o oracle) Options(context.Context, string, string, info.VariantSet, ...string) (map[string]string, error) {
	return nil, nil
}
func (o oracle) FetchInfo(context.Context, string, string, info.VariantSet, bool) (info.FetchInfo, error) {
	return info.FetchInfo{}, nil
}
func (o oracle) Globals(context.Context, string, string, info.VariantSet) (map[string]string, error) {
	return nil, nil
}

var _ port.Oracle = oracle{}

// fetcher is the network seam, and it records whether it was handed on.
type fetcher struct{ used bool }

func (f *fetcher) Fetch(context.Context, []string, distfile.Options, string) (checksums.Sums, error) {
	f.used = true
	return checksums.Sums{}, nil
}

// recorder is a planner that answers with a plan naming what it was
// given, so a test can see which handle and which fetcher reached it.
type recorder struct {
	name    string
	handle  port.Handle
	fetched bool
	err     error
}

func (r *recorder) Plan(_ context.Context, h port.Handle, f distfile.Fetcher) (*plan.Plan, error) {
	r.handle, r.fetched = h, f != nil
	if r.err != nil {
		return nil, r.err
	}
	return &plan.Plan{Intent: r.name, Port: "jq", Slug: "jq-1.8"}, nil
}

func catalogue(rec *recorder, fetches bool) []intent.Definition {
	return []intent.Definition{{
		Name:    "bump",
		Aliases: []string{"b"},
		Fetches: fetches,
		New:     func(intent.Params) (intent.Planner, error) { return rec, nil },
	}}
}

func target() tree.Target { return tree.Target{Portdir: "/ports/sysutils/jq"} }

// THE CATALOGUE IS DATA AND THE ROAD DOES NOT BRANCH ON WHICH INTENT IS
// RUNNING: a verb is looked up, its planner is built, and the plan comes
// back. An alias resolves to the same entry.
func TestPlanRunsTheCatalogueEntryTheVerbNames(t *testing.T) {
	rec := &recorder{name: "bump"}
	p := Planner{Eval: oracle{}, Catalog: catalogue(rec, false)}

	got, err := p.Plan(t.Context(), "bump", target(), intent.Params{Target: "jq"})
	require.NoError(t, err)
	assert.Equal(t, "jq-1.8", got.Slug)
	assert.Equal(t, "/ports/sysutils/jq", rec.handle.Target.Portdir)

	got, err = p.Plan(t.Context(), "b", target(), intent.Params{Target: "jq"})
	require.NoError(t, err)
	assert.Equal(t, "jq-1.8", got.Slug, "an alias is the same entry")
}

// A VERB THE CATALOGUE DOES NOT CARRY IS A SENTINEL, because the caller
// that meets it is a composition root wiring a verb that does not exist.
func TestPlanRefusesAnUnknownVerb(t *testing.T) {
	p := Planner{Eval: oracle{}, Catalog: catalogue(&recorder{}, false)}
	_, err := p.Plan(t.Context(), "promote", target(), intent.Params{})
	require.ErrorIs(t, err, ErrNoIntent)
	assert.Contains(t, err.Error(), "promote", "the refusal names what was asked for")
}

// A PLANNER WITH NO EVALUATOR PRODUCES NO PLAN, rather than an empty
// one: the zero value must not mean both "nothing to plan" and "nobody
// wired an evaluator" (rule 7).
func TestPlanRefusesWithNoEvaluator(t *testing.T) {
	p := Planner{Catalog: catalogue(&recorder{}, false)}
	_, err := p.Plan(t.Context(), "bump", target(), intent.Params{})
	require.ErrorIs(t, err, ErrNoEvaluator)
}

// THE FETCHER IS HANDED ON ONLY TO AN INTENT THAT FETCHES, and nil stays
// a nil INTERFACE — not a typed nil in disguise, which the planner's own
// nil test would pass and then dereference.
func TestFetcherIsHandedOnOnlyWhenTheIntentFetches(t *testing.T) {
	rec := &recorder{name: "bump-revision"}
	f := &fetcher{}
	quiet := Planner{Eval: oracle{}, Fetch: f, Catalog: catalogue(rec, false)}
	_, err := quiet.Plan(t.Context(), "bump", target(), intent.Params{})
	require.NoError(t, err)
	assert.False(t, rec.fetched, "a revision bump downloads nothing to count")

	loud := Planner{Eval: oracle{}, Fetch: f, Catalog: catalogue(rec, true)}
	_, err = loud.Plan(t.Context(), "bump", target(), intent.Params{})
	require.NoError(t, err)
	assert.True(t, rec.fetched)
}

// AN INTENT THAT READS THE NETWORK WITH NO FETCHER IS REFUSED, because a
// bump that silently planned no checksums would be a plan that lied
// about what it wrote.
func TestPlanRefusesAFetchingIntentWithNoFetcher(t *testing.T) {
	p := Planner{Eval: oracle{}, Catalog: catalogue(&recorder{}, true)}
	_, err := p.Plan(t.Context(), "bump", target(), intent.Params{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no fetcher")
}

// --riders MAKES HOUSEKEEPING THE WHOLE CHANGE: the catalogue's planner
// is replaced, and no fetcher is acquired even for an intent that
// fetches, because the verb chose the port and nothing else of it runs.
func TestRidersOnlySwapsThePlannerAndSkipsTheFetcher(t *testing.T) {
	rec := &recorder{name: "bump"}
	f := &fetcher{}
	p := Planner{Eval: oracle{}, Fetch: f, Catalog: catalogue(rec, true)}

	// The housekeeping planner reads the Portfile off the handle, which
	// this fixture has no file for, so what is proven here is that the
	// CATALOGUE's planner never ran and the fetcher was never used.
	_, _ = p.Plan(t.Context(), "bump", target(), intent.Params{Riders: intent.RidersOnly})
	assert.Empty(t, rec.handle.Target.Portdir, "the catalogue's planner was not the one that ran")
	assert.False(t, f.used)
}

// A PLANNER'S OWN REFUSAL COMES BACK UNWRAPPED: a decline carries its own
// exit band, and a sentence from this layer would put a second author's
// words in front of the planner's.
func TestPlanReturnsThePlannersRefusalAsItself(t *testing.T) {
	boom := errors.New("nothing to do")
	rec := &recorder{err: boom}
	p := Planner{Eval: oracle{}, Catalog: catalogue(rec, false)}
	_, err := p.Plan(t.Context(), "bump", target(), intent.Params{})
	require.ErrorIs(t, err, boom)
	assert.Equal(t, boom.Error(), err.Error(), "unwrapped, not re-narrated")
}

// Verbs names what the catalogue answers to, so a refusal can say what
// was available without the caller holding the catalogue.
func TestVerbsNamesEveryEntryAndItsAliases(t *testing.T) {
	p := Planner{Catalog: catalogue(&recorder{}, false)}
	assert.Equal(t, []string{"bump", "b"}, p.Verbs())
	assert.Contains(t, p.String(), "bump b")
	assert.Empty(t, Planner{}.Verbs(), "an empty catalogue is a wiring gap a caller can see")
}
