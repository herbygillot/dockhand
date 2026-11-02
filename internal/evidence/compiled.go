package evidence

import "github.com/herbygillot/dockhand/internal/model"

// LogSpan is where one build is in its log: its lines from From to the
// line before To, or to the log's end where To is 0.
type LogSpan struct {
	Log      string
	From, To int
}

// BuildSpans are where a port's builds are in their kept logs, one for
// each environment that recorded a result with a log, for reading what
// they compiled (buildlog.Compiled; the omnidiff session's note 6): from
// the install step, which builds it, or its own build's first step where
// none was recorded, or the log's start, to the next step of it, or of
// another target in the same log, after. A port is its targets' subport,
// else their name.
func (e Evidence) BuildSpans(port string) []LogSpan {
	var spans []LogSpan
	for _, target := range e.Targets {
		if built := target.Target.Target; built.Subport != port && (built.Subport != "" || built.Name != port) {
			continue
		}
		for _, cell := range target.Outcomes {
			if cell.Kind != CellRecorded || cell.Log == "" {
				continue
			}
			span := LogSpan{Log: cell.Log, From: 1}
			if own, ok := cell.OwnBuild(); ok {
				span.From = own.Line
			}
			for _, step := range cell.Steps {
				if step.Name == model.StepInstall {
					span.From = step.Line
				}
			}
			for _, other := range e.Targets {
				for _, them := range other.Outcomes {
					if them.Log != cell.Log {
						continue
					}
					for _, step := range them.Steps {
						if step.Line > span.From && (span.To == 0 || step.Line < span.To) {
							span.To = step.Line
						}
					}
				}
			}
			spans = append(spans, span)
		}
	}
	return spans
}
