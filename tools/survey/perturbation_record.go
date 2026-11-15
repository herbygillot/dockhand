package main

import (
	"encoding/json"

	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/model"
)

type probeCounts struct {
	Perturbed, Same, BuildOnlyMoved, Differs, Failed, NotPerturbed int
	ModeledFollowerNotPerturbed                                    int
	UnknownSubstitutions                                           int
	Seconds                                                        float64
}

func (c *probeCounts) add(p probeSummary, modeledExplained bool) {
	switch p.Status {
	case probeSame:
		c.Same++
	case probeBuildOnlyMoved:
		c.BuildOnlyMoved++
	case probeDiffers:
		c.Differs++
	case probeFailed:
		c.Failed++
	case probeNotPerturbed:
		c.NotPerturbed++
		if modeledExplained && p.FollowerBenign {
			c.ModeledFollowerNotPerturbed++
		}
	}
	if p.Standins > 0 {
		c.Perturbed++
	} else if !p.SubstitutionsKnown {
		c.UnknownSubstitutions++
	}
	c.Seconds += p.Seconds
}

type probeFinding struct {
	Evaluation       int64
	Port             string
	Platform         model.Platform
	Field, ErrorCode string
}

type probeReport struct {
	UnperturbedClosedReads map[string]int
	Total                  probeCounts
	ByPort                 map[string]probeCounts
	ByRule                 map[observe.Rule]probeCounts
	// Each evaluation identifies its lossless capture containing both plans.
	Findings []probeFinding `json:",omitempty"`
}

func (r *Record) probeReport() (*probeReport, error) {
	rows, err := r.db.Query(`select e.id,p.name,e.platform_os,e.platform_version,e.platform_arch,t.status,t.field,t.error_code,t.seconds,t.standins,t.follower_benign,t.rules,t.substitutions_known,t.build_only_moved,e.modeled,c.complete,c.on_verdict,t.unperturbed_closed_reads from tolerance_probe t join tolerance_context c on c.evaluation_id=t.evaluation_id join evaluation e on e.id=t.evaluation_id join port p on p.id=e.port_id where e.run_id=? order by e.id`, r.run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out *probeReport
	for rows.Next() {
		var f probeFinding
		var status, rulesJSON string
		var seconds float64
		var standins int
		var benign, known, modeled, complete, inconclusive bool
		var movedJSON, closedJSON string
		if err = rows.Scan(&f.Evaluation, &f.Port, &f.Platform.OS, &f.Platform.Version, &f.Platform.Architecture, &status, &f.Field, &f.ErrorCode, &seconds, &standins, &benign, &rulesJSON, &known, &movedJSON, &modeled, &complete, &inconclusive, &closedJSON); err != nil {
			return nil, err
		}
		if out == nil {
			out = &probeReport{UnperturbedClosedReads: map[string]int{}, ByPort: map[string]probeCounts{}, ByRule: map[observe.Rule]probeCounts{}}
		}
		p := probeSummary{Status: status, Standins: standins, Seconds: seconds, FollowerBenign: benign, SubstitutionsKnown: known}
		if err = json.Unmarshal([]byte(movedJSON), &p.BuildOnlyMoved); err != nil {
			return nil, err
		}
		if benign && complete && !inconclusive {
			var calls []string
			if err = json.Unmarshal([]byte(closedJSON), &calls); err != nil {
				return nil, err
			}
			// Deduplicate defensively even when reading a manually made record.
			seen := map[string]bool{}
			for _, call := range calls {
				if !seen[call] {
					out.UnperturbedClosedReads[call]++
					seen[call] = true
				}
			}
		}
		out.Total.add(p, modeled && complete && !inconclusive)
		port := out.ByPort[f.Port]
		port.add(p, modeled && complete && !inconclusive)
		out.ByPort[f.Port] = port
		var rules []observe.Rule
		if err = json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
			return nil, err
		}
		for _, rule := range rules {
			counts := out.ByRule[rule]
			counts.add(p, modeled && complete && !inconclusive)
			out.ByRule[rule] = counts
		}
		if status == "differs" && benign {
			out.Findings = append(out.Findings, f)
		}
	}
	return out, rows.Err()
}
