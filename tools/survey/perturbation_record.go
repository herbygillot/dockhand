package main

import (
	"encoding/json"

	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/model"
)

type probeCounts struct {
	Same, Differs, Failed, Unperturbed int
	Seconds                            float64
}

func (c *probeCounts) add(status string, standins int, seconds float64) {
	switch status {
	case "same":
		c.Same++
	case "differs":
		c.Differs++
	case "failed":
		c.Failed++
	}
	if standins == 0 {
		c.Unperturbed++
	}
	c.Seconds += seconds
}

type probeFinding struct {
	Evaluation       int64
	Port             string
	Platform         model.Platform
	Field, ErrorCode string
}

type probeReport struct {
	Total  probeCounts
	ByPort map[string]probeCounts
	ByRule map[observe.Rule]probeCounts
	// Each evaluation identifies its lossless capture containing both plans.
	Findings []probeFinding `json:",omitempty"`
}

func (r *Record) probeReport() (*probeReport, error) {
	rows, err := r.db.Query(`select e.id,p.name,e.platform_os,e.platform_version,e.platform_arch,t.status,t.field,t.error_code,t.seconds,t.standins,t.follower_benign,t.rules from tolerance_probe t join evaluation e on e.id=t.evaluation_id join port p on p.id=e.port_id where e.run_id=? order by e.id`, r.run)
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
		var benign bool
		if err = rows.Scan(&f.Evaluation, &f.Port, &f.Platform.OS, &f.Platform.Version, &f.Platform.Architecture, &status, &f.Field, &f.ErrorCode, &seconds, &standins, &benign, &rulesJSON); err != nil {
			return nil, err
		}
		if out == nil {
			out = &probeReport{ByPort: map[string]probeCounts{}, ByRule: map[observe.Rule]probeCounts{}}
		}
		out.Total.add(status, standins, seconds)
		port := out.ByPort[f.Port]
		port.add(status, standins, seconds)
		out.ByPort[f.Port] = port
		var rules []observe.Rule
		if err = json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
			return nil, err
		}
		for _, rule := range rules {
			counts := out.ByRule[rule]
			counts.add(status, standins, seconds)
			out.ByRule[rule] = counts
		}
		if status == "differs" && benign {
			out.Findings = append(out.Findings, f)
		}
	}
	return out, rows.Err()
}
