package main

import (
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// experimentContext reports product evidence. The survey never evaluates a
// stand-in, applies a separate policy, or receives a second observation.
type experimentContext struct {
	Tolerance *toleranceVerdict `json:",omitempty"`
	Platform  model.Platform
	Modeled   bool
	Summary   macports.MachineReadExperiment
}

func experimentReports(observations []observed) []experimentContext {
	var reports []experimentContext
	for _, o := range observations {
		summary := macports.MachineReadExperiment{Outcome: "not requested"}
		if !o.Modeled {
			summary.Outcome = "not perturbed"
		}
		if o.Err != nil {
			summary.Outcome = "failed"
			summary.ErrorCode = "evaluation-unavailable"
		}
		if o.Observation.MachineReadExperiment != nil {
			summary = *o.Observation.MachineReadExperiment
		}
		reports = append(reports, experimentContext{Platform: o.Platform, Modeled: o.Modeled, Summary: summary, Tolerance: o.Tolerance})
	}
	return reports
}
