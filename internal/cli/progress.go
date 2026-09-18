package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
)

type reporter struct {
	providers map[string]verify.Provider
	out       io.Writer
	logs      verify.LogReader
	trace     bool
	level     progress.Level
	jsonMode  bool
	last      map[string]string
	offsets   map[record.ProviderRun]int64
}

func newReporter(out io.Writer, p verify.Provider, trace bool, level progress.Level, jsonMode bool) *reporter {
	logs, _ := p.(verify.LogReader)
	return &reporter{out: out, logs: logs, trace: trace, level: level, jsonMode: jsonMode, last: map[string]string{}, offsets: map[record.ProviderRun]int64{}}
}

// label names a job for a person: its port at the info level, its job ID
// when identifiers were asked for.
func (r *reporter) label(job record.Job) string {
	if r.level >= progress.Verbose || len(job.Spec.Targets) == 0 {
		return string(job.ID)
	}
	return job.Spec.Targets[0].Name
}
func plain(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strconv.Quote(s), "\""), "\"")
}
func (r *reporter) changed(key, value string) error {
	if r.last[key] == value {
		return nil
	}
	r.last[key] = value
	if r.jsonMode {
		return json.NewEncoder(r.out).Encode(struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}{"info", value})
	}
	_, err := fmt.Fprintln(r.out, plain(value))
	return err
}
func (r *reporter) cycle(result workflow.CycleResult) error {
	for _, problem := range result.Problems {
		id := string(problem.JobID)
		if id == "" {
			id = string(problem.ResourceID)
		}
		if err := r.changed("problem:"+id, fmt.Sprintf("%s: %s", id, problem.Detail)); err != nil {
			return err
		}
	}
	return nil
}
func (r *reporter) status(ctx context.Context, status workflow.Status) error {
	for _, entry := range status.Jobs {
		message := fmt.Sprintf("%s: %s", r.label(entry.Job), entry.Job.State)
		if entry.Job.ReuseDetail != "" && entry.Job.State != record.JobCompleted {
			if err := r.changed("reuse:"+string(entry.Job.ID), fmt.Sprintf("%s: %s", r.label(entry.Job), entry.Job.ReuseDetail)); err != nil {
				return err
			}
		}
		if outcome := completedOutcome(entry); outcome != "" {
			message += "; " + outcome
		}
		if entry.Job.Detail != "" && entry.Job.Detail != entry.Job.ReuseDetail {
			message += "; " + entry.Job.Detail
		}
		waiting := 0
		for _, attempt := range entry.Attempts {
			if attempt.State == record.AttemptQueued {
				waiting++
			}
			if attempt.LastError != "" && attempt.LastError != entry.Job.Detail {
				message += "; " + attempt.LastError
			}
		}
		if waiting == 1 {
			message += "; waiting for provider admission"
		} else if waiting > 1 {
			message += fmt.Sprintf("; %d targets waiting for provider admission", waiting)
		}
		if err := r.changed("job:"+string(entry.Job.ID), message); err != nil {
			return err
		}
		if !r.trace || entry.Job.ReusedAttempt != "" {
			continue
		}
		for _, attempt := range entry.Attempts {
			if attempt.Run.RunID == "" {
				continue
			}
			logs := r.logs
			if provider := r.providers[attempt.Run.Provider]; provider != nil {
				logs, _ = provider.(verify.LogReader)
			}
			if logs == nil {
				return fmt.Errorf("trace: provider %s does not support log reading", attempt.Run.Provider)
			}
			terminal := attempt.State == record.AttemptFinished || attempt.State == record.AttemptCanceled
			for {
				call, cancel := context.WithTimeout(ctx, 10*time.Second)
				chunk, err := logs.ReadLog(call, attempt.Run, r.offsets[attempt.Run], 65536)
				cancel()
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if e := r.changed("log:"+attempt.Run.RunID, "Log read unavailable: "+err.Error()); e != nil {
						return e
					}
					break
				}
				if chunk.Next != r.offsets[attempt.Run]+int64(len(chunk.Data)) {
					return fmt.Errorf("trace: invalid log offset")
				}
				r.offsets[attempt.Run] = chunk.Next
				if len(chunk.Data) > 0 {
					text := strings.Map(func(c rune) rune {
						if c < 32 && c != '\n' && c != '\t' || c == 127 {
							return -1
						}
						return c
					}, string(chunk.Data))
					if _, err = io.WriteString(r.out, text); err != nil {
						return err
					}
				}
				if !terminal || chunk.Complete || len(chunk.Data) == 0 {
					break
				}
			}
		}
	}
	return nil
}

func completedOutcome(entry view.JobStatus) string {
	job := entry.Job
	if job.State != record.JobCompleted {
		if job.State.Terminal() && job.Phase == record.PhasePreparation && job.ResultRevision == "" {
			if job.Prepared != nil {
				return "preparation stopped; candidate branch integration is unconfirmed"
			}
			return "preparation stopped; no update branch was created"
		}
		return ""
	}
	if job.ResolvedRelease != nil && job.ResolvedRelease.NoUpdate {
		return fmt.Sprintf("already current at %s; no update branch or build needed", job.ResolvedRelease.CurrentVersion)
	}
	if job.Spec.Destination == record.BranchReady && job.ResultRevision != "" {
		return "update branch prepared; this job did not request verification"
	}
	if entry.Job.Spec.Destination == record.Published && len(entry.Publications) > 0 && entry.Publications[0].State == record.PublicationConfirmed {
		return "publication confirmed"
	}
	if entry.Reused != nil && entry.Reused.Evidence != nil && entry.Reused.Evidence.Verdict == record.VerdictPassed {
		return verificationOutcome(entry.Job) + " (reused)"
	}
	if len(entry.Attempts) == 0 {
		return ""
	}
	for _, attempt := range entry.Attempts {
		if attempt.State != record.AttemptFinished || attempt.Evidence == nil || attempt.Evidence.Verdict != record.VerdictPassed {
			return ""
		}
	}
	return verificationOutcome(entry.Job)
}

// level resolves the report level from -v, --debug, and a command's --trace.
func (r *runtime) level(cmd *cobra.Command) progress.Level {
	level := progress.Level(min(r.verbosity, 2))
	if r.debug {
		level = progress.Debug
	}
	if flag := cmd.Flags().Lookup("trace"); flag != nil && flag.Value.String() == "true" {
		level = progress.Debug
	}
	return level
}

// progressContext prints reports at or below level. In JSON mode each report
// is one JSON object per line on stderr, so stdout stays the result.
func progressContext(ctx context.Context, out io.Writer, level progress.Level, jsonMode bool) context.Context {
	var last progress.Update
	return progress.WithReporter(ctx, func(update progress.Update) {
		if update == last || update.Level > level {
			return
		}
		last = update
		if jsonMode {
			_ = json.NewEncoder(out).Encode(struct {
				Level   string `json:"level"`
				Scope   string `json:"scope,omitempty"`
				Message string `json:"message"`
			}{update.Level.String(), update.Scope, update.Message})
			return
		}
		message := update.Message
		if update.Scope != "" {
			message = update.Scope + ": " + message
		}
		_, _ = fmt.Fprintln(out, plain(message))
	})
}

func verificationOutcome(job record.Job) string {
	if job.Spec.Action == record.Verify && job.ChangeID == "" {
		return "verification passed for standalone source; no update was prepared"
	}
	if job.ResultRevision != "" {
		return "verification passed for prepared update (recorded result)"
	}
	return "verification passed"
}
