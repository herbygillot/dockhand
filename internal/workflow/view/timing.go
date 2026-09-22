package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

// Took words the time between two moments the way a person reads a
// duration off a clock: seconds under a minute, minutes and seconds under
// an hour, hours and minutes beyond. A gap under a second is "under 1s".
func Took(from, to time.Time) string {
	d := to.Sub(from).Round(time.Second)
	switch {
	case d < 0:
		return ""
	case d < time.Second:
		return "under 1s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int(d%time.Minute/time.Second))
	}
	return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
}

// Phase is how long one phase of a job took, from the record.
type Phase struct {
	Name string
	Took string
}

// Phases are a job's phase durations as the record holds them, in order:
// preparation from acceptance to the branch integrated, verification from
// an attempt's creation to its verdict, publication from the last verdict,
// or the branch, to confirmation, and the whole job from acceptance to its
// finish. A phase whose end the record does not hold is left out, so a
// running job lists what has finished.
func Phases(entry JobStatus) []Phase {
	job := entry.Job
	var phases []Phase
	last := job.AcceptedAt
	if job.Prepared != nil && job.Prepared.IntegratedAt != nil {
		phases = append(phases, Phase{"preparation", Took(job.AcceptedAt, *job.Prepared.IntegratedAt)})
		last = *job.Prepared.IntegratedAt
	}
	for _, attempt := range entry.Attempts {
		if attempt.Evidence == nil || attempt.CreatedAt.IsZero() || attempt.Evidence.ObservedAt.IsZero() {
			continue
		}
		switch attempt.Evidence.Verdict {
		case record.VerdictPassed, record.VerdictFailed, record.VerdictBlocked, record.VerdictErrored, record.VerdictUnsupported:
		default:
			continue
		}
		name := "verification"
		if len(entry.Attempts) > 1 && attempt.Spec.Target.Name != "" {
			name = "verification of " + attempt.Spec.Target.Name
		}
		phases = append(phases, Phase{name, Took(attempt.CreatedAt, attempt.Evidence.ObservedAt)})
		if attempt.Evidence.ObservedAt.After(last) {
			last = attempt.Evidence.ObservedAt
		}
	}
	for _, publication := range entry.Publications {
		if publication.ConfirmedAt != nil {
			phases = append(phases, Phase{"publication", Took(last, *publication.ConfirmedAt)})
			break
		}
	}
	if job.FinishedAt != nil && !job.AcceptedAt.IsZero() {
		phases = append(phases, Phase{"in all", Took(job.AcceptedAt, *job.FinishedAt)})
	}
	return phases
}

// PhaseWords is one line for a job's phase durations: "took 35m02s:
// preparation 2m14s, verification 31m40s, publication 12s", and nothing
// when the record holds no finished phase.
func PhaseWords(entry JobStatus) string {
	phases := Phases(entry)
	if len(phases) == 0 {
		return ""
	}
	var parts []string
	total := ""
	for _, phase := range phases {
		if phase.Name == "in all" {
			total = phase.Took
			continue
		}
		parts = append(parts, phase.Name+" "+phase.Took)
	}
	switch {
	case total != "" && len(parts) > 0:
		return "took " + total + ": " + strings.Join(parts, ", ")
	case total != "":
		return "took " + total
	}
	return "took " + strings.Join(parts, ", ")
}
