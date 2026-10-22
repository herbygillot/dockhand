package engine

import (
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
	var results []model.TargetResult
	declared := map[model.TargetID][]string{}
	for _, target := range checked.Targets {
		for _, cell := range target.Outcomes {
			reader, ok := e.Providers[cell.Environment.Provider].(buildenv.LinkReader)
			if !ok || !reader.ReadsLinks() || cell.Outcome != model.OutcomePassed {
				continue
			}
			result := cell.TargetResult
			result.Target = target.Target.ID
			infos, ok := ports(target.Target, cell.Environment)
			if !ok {
				result.Links = nil
			}
			results = append(results, result)
			for _, info := range infos {
				declared[target.Target.ID] = appendOnce(declared[target.Target.ID], info.Name)
				if !strings.EqualFold(info.Name, builtPort(target.Target.Target)) {
					continue
				}
				for _, dependency := range info.Dependencies {
					if dependency.Phase == "lib" && dependency.Port != "" {
						declared[target.Target.ID] = appendOnce(declared[target.Target.ID], dependency.Port)
					}
				}
			}
		}
	}
	return evidence.UndeclaredLinks(results, declared), nil
}

// builtPort is the port a target builds: its subport, else its name.
func builtPort(target model.Target) string {
	if target.Subport != "" {
		return target.Subport
	}
	return target.Name
}
