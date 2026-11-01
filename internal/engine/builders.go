package engine

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/herbygillot/dockhand/internal/macports/buildhistory"
	"github.com/herbygillot/dockhand/internal/model"
)

// BuilderHistory reads what MacPorts' builders made of a port, as
// ports.macports.org keeps it (buildhistory.Reader): false where the site
// gave no answer, which says nothing.
type BuilderHistory interface {
	History(ctx context.Context, port string) (buildhistory.History, bool)
}

// buildersAsked is how many ports' histories are asked at once, as many
// as HTTPS answers are.
const buildersAsked = httpsAsks

// PortBuilds is what MacPorts' builders made of a changed port, by their
// latest build on each release (buildhistory.History.Words).
type PortBuilds struct {
	Port    string
	History buildhistory.History
}

// builderHistories are the ports' build histories, in the ports' order,
// each read once, several at once; a port whose history the site didn't
// give is left out, and none is read without a BuilderHistory.
func (e *Engine) builderHistories(ctx context.Context, ports []string) []PortBuilds {
	if e.Builders == nil || len(ports) == 0 {
		return nil
	}
	histories := make([]buildhistory.History, len(ports))
	read := make([]bool, len(ports))
	slots := make(chan struct{}, buildersAsked)
	var wait sync.WaitGroup
	for i, port := range ports {
		wait.Add(1)
		go func() {
			defer wait.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			histories[i], read[i] = e.Builders.History(ctx, port)
		}()
	}
	wait.Wait()
	var builds []PortBuilds
	for i, port := range ports {
		if read[i] {
			builds = append(builds, PortBuilds{Port: port, History: histories[i]})
		}
	}
	return builds
}

// BuildersBeside say, beside a failed check's offer of a baseline, what
// MacPorts' builders made of each offered port on each release it failed
// on here, by their latest build there: "MacPorts' builders failed exiv2
// on macOS 15 arm64 too, at their latest build there, 2026-10-25
// (ports.macports.org)". Where they failed the same release, the failure
// predates the branch, which can save the baseline's build; where they
// built it, the branch's change or this environment is the likelier
// cause. A port's history is read once; nothing is said where the site
// gave no answer, or of a release its builders never built the port on.
func (e *Engine) BuildersBeside(ctx context.Context, run model.Run, ports []string) []string {
	if e.Builders == nil || len(ports) == 0 {
		return nil
	}
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return nil
	}
	// failedOn are the builders each port failed here on, in the order
	// met.
	failedOn := map[string][]string{}
	var order []string
	for _, target := range evidence.Failed() {
		port := target.Target.Target.Name
		if !slices.Contains(ports, port) && !slices.Contains(ports, string(target.Target.ID)) {
			continue
		}
		for n, cell := range target.Outcomes {
			if cell.Outcome != model.OutcomeFailed {
				continue
			}
			builder, ok := buildhistory.Builder(evidence.Plan.Environments[n].Platform)
			if !ok || slices.Contains(failedOn[port], builder) {
				continue
			}
			if _, met := failedOn[port]; !met {
				order = append(order, port)
			}
			failedOn[port] = append(failedOn[port], builder)
		}
	}
	var words []string
	for _, builds := range e.builderHistories(ctx, order) {
		for _, builder := range failedOn[builds.Port] {
			latest, ok := builds.History.Latest(builder)
			if !ok {
				continue
			}
			release, started := buildhistory.Release(builder), latest.Started.UTC().Format("2006-01-02")
			if latest.Built() {
				words = append(words, fmt.Sprintf("MacPorts' builders built %s on %s at their latest build there, %s (ports.macports.org)", builds.Port, release, started))
			} else {
				words = append(words, fmt.Sprintf("MacPorts' builders %s %s on %s too, at their latest build there, %s (ports.macports.org)", builderVerb(latest), builds.Port, release, started))
			}
		}
	}
	return words
}

// builderVerb is how a builder's failed build is said before the port's
// name, "failed", or "failed the dependencies of".
func builderVerb(build buildhistory.Build) string {
	if build.AtDependencies() {
		return "failed the dependencies of"
	}
	return "failed"
}
