package engine

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// LinkFinding is evidence's (evidence.LinkFinding), for the command line
// to word.
type LinkFinding = evidence.LinkFinding

// LintFinding is evidence's (evidence.LintFinding), for the command line.
type LintFinding = evidence.LintFinding

// UndeclaredLinks are what a check's passed builds link that their ports
// don't declare in depends_lib (v0.5.0 part 6), the rule evidence's
// (evidence.UndeclaredLinks): this gathers its results' links, from the
// providers that read them (buildenv.LinkReader), and what each target
// declares,
// its depends_lib as the check's revision evaluates it in the result's
// environment, with itself and its Portfile's other subports. A port that
// can't be evaluated declares nothing that can be read, so its links are
// said unread rather than guessed at.
func (e *Engine) UndeclaredLinks(ctx context.Context, run model.Run) ([]evidence.LinkFinding, error) {
	checked, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return nil, err
	}
	// What each target declares is its Portfile's evaluation, which the
	// trust rule lets unattended work make of the person's alone.
	if run.Branch != "" {
		if branch, err := e.Branch(ctx, run.Branch); err == nil && !e.mayEvaluate(ctx, branch, "link scan") {
			return nil, nil
		}
	}
	reader, err := e.portReader()
	if err != nil {
		return nil, nil
	}
	evaluated := map[string][]macports.PortInfo{}
	ports := func(target model.PlanTarget, environment model.Environment) ([]macports.PortInfo, bool) {
		key := fmt.Sprintf("%s\x00%s\x00%v", target.Directory, target.Target.VariantSpec(), environment)
		if found, ok := evaluated[key]; ok {
			return found, found != nil
		}
		found, err := reader.Ports(ctx, revision.Source, target.Directory, environment, target.Target.Variants)
		if err != nil {
			found = nil
		}
		evaluated[key] = found
		return found, found != nil
	}
	var builds []evidence.LinkedBuild
	for _, target := range checked.Targets {
		for _, cell := range target.Outcomes {
			reader, ok := e.Providers[cell.Environment.Provider].(buildenv.LinkReader)
			if !ok || !reader.ReadsLinks() || cell.Outcome != model.OutcomePassed {
				continue
			}
			result := cell.TargetResult
			result.Target = target.Target.ID
			infos, ok := ports(target.Target, cell.Environment)
			unread := ""
			switch {
			case !ok:
				result.Links, unread = nil, "its Portfile couldn't be evaluated there, so what it declares isn't known"
			case result.Links == nil && result.ReusedFrom != "":
				// A reused result carries the reading of the check that
				// built it, which may predate the scan (the bazel
				// session's notes, 2026-10-24).
				unread = fmt.Sprintf("its result is %s's, which didn't read them", cmp.Or(checked.CellCheck(cell), "an earlier check"))
			case result.Links == nil:
				unread = "the guest couldn't read them, as its log says"
			}
			// What this environment declares, judged with its own build
			// alone (Codex's review of 8c007979, C8-2).
			var declared []string
			for _, info := range infos {
				declared = appendOnce(declared, info.Name)
				if !strings.EqualFold(info.Name, builtPort(target.Target.Target)) {
					continue
				}
				for _, dependency := range info.Dependencies {
					if dependency.Phase == "lib" && dependency.Port != "" {
						declared = appendOnce(declared, dependency.Port)
					}
				}
			}
			builds = append(builds, evidence.LinkedBuild{Result: result, Environment: cell.Environment, Declared: declared, Unread: unread})
		}
	}
	return evidence.UndeclaredLinks(builds), nil
}

// builtPort is the port a target builds: its subport, else its name.
func builtPort(target model.Target) string {
	if target.Subport != "" {
		return target.Subport
	}
	return target.Name
}

// LintFindings are what lint warned of in a check's evidence
// (evidence.LintFindings).
func LintFindings(checked Evidence) []LintFinding { return evidence.LintFindings(checked) }
