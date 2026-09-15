package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
)

type reporter struct {
	providers map[string]verify.Provider
	out       io.Writer
	logs      verify.LogReader
	trace     bool
	last      map[string]string
	offsets   map[record.ProviderRun]int64
}

func newReporter(out io.Writer, p verify.Provider, trace bool) *reporter {
	logs, _ := p.(verify.LogReader)
	return &reporter{out: out, logs: logs, trace: trace, last: map[string]string{}, offsets: map[record.ProviderRun]int64{}}
}
func plain(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strconv.Quote(s), "\""), "\"")
}
func (r *reporter) changed(key, value string) error {
	if r.last[key] == value {
		return nil
	}
	r.last[key] = value
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
		message := fmt.Sprintf("%s: %s", entry.Job.ID, entry.Job.State)
		if entry.Job.ReuseDetail != "" && entry.Job.State != record.JobCompleted {
			if err := r.changed("reuse:"+string(entry.Job.ID), fmt.Sprintf("%s: %s", entry.Job.ID, entry.Job.ReuseDetail)); err != nil {
				return err
			}
		}
		if outcome := completedOutcome(entry); outcome != "" {
			message += "; " + outcome
		}
		if entry.Job.Detail != "" && entry.Job.Detail != entry.Job.ReuseDetail {
			message += "; " + entry.Job.Detail
		}
		for _, attempt := range entry.Attempts {
			if attempt.State == record.AttemptQueued {
				message += "; waiting for provider admission"
			}
			if attempt.LastError != "" && attempt.LastError != entry.Job.Detail {
				message += "; " + attempt.LastError
			}
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

func completedOutcome(entry workflow.JobStatus) string {
	if entry.Job.State != record.JobCompleted {
		return ""
	}
	if entry.Job.Spec.Destination == record.Published && len(entry.Publications) > 0 && entry.Publications[0].State == record.PublicationConfirmed {
		return "publication confirmed"
	}
	if entry.Reused != nil && entry.Reused.Evidence != nil && entry.Reused.Evidence.Verdict == record.VerdictPassed {
		return "verification passed (reused)"
	}
	if len(entry.Attempts) == 0 {
		return ""
	}
	for _, attempt := range entry.Attempts {
		if attempt.State != record.AttemptFinished || attempt.Evidence == nil || attempt.Evidence.Verdict != record.VerdictPassed {
			return ""
		}
	}
	return "verification passed"
}

func progressContext(ctx context.Context, out io.Writer) context.Context {
	var last progress.Update
	return progress.WithReporter(ctx, func(update progress.Update) {
		if update == last {
			return
		}
		last = update
		message := update.Message
		if update.Scope != "" {
			message = update.Scope + ": " + message
		}
		_, _ = fmt.Fprintln(out, plain(message))
	})
}
