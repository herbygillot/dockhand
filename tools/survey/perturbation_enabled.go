//go:build surveymeasure

package main

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/model"
)

func perturbContext(ctx context.Context, observer macports.Observer, sourceIdentity model.Source, native model.Platform, info macports.PortInfo, c *toleranceCapture) *probeResult {
	rules, benign := probeRules(c)
	if len(rules) == 0 {
		return nil
	}
	r := &probeResult{Version: probeVersion, Status: "failed", Rules: rules, FollowerBenign: benign, Before: makeProbePlan(info, c.Observation)}
	started := time.Now()
	defer func() { r.Seconds = time.Since(started).Seconds() }()
	if !c.Pair.Complete {
		r.ErrorCode = "probe-baseline-incomplete"
		return r
	}
	tree, err := macports.NewTree(sourceIdentity, c.Root, native)
	if err != nil {
		r.ErrorCode = "probe-source-unavailable"
		r.Error = err.Error()
		return r
	}
	source, err := tree.Select(c.Target)
	if err != nil {
		r.ErrorCode = "probe-source-unavailable"
		r.Error = err.Error()
		return r
	}
	got, err := observer.Observe(eval.WithStandinAnswers(ctx), source, macports.ObservationRequest{Platform: c.Platform, Declarations: true, SelectedOnly: true})
	if err == nil && got.Unanswered != "" {
		err = &macports.UnansweredError{Call: got.Unanswered}
	}
	if err != nil {
		probeFailure(r, err)
		return r
	}
	after, ok := got.Ports[c.Target.Name]
	next, hasInfo := got.Snapshot.Ports[c.Target.Name]
	if !ok || !hasInfo {
		r.ErrorCode = "probe-port-missing"
		return r
	}
	r.Observation = &after
	r.UnperturbedClosedReads = unperturbedClosedReads(after)
	plan := makeProbePlan(next, after)
	r.After = &plan
	for _, e := range after.Ledger {
		if e.Source == "standin" && e.Subject != "fingerprint" {
			r.Standins += e.Count
		}
	}
	classifyProbePlans(r)
	return r
}
