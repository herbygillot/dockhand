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

// status reports what changed for the jobs a command is attached to. At the
// info level it narrates the milestones a person waits for, in the words
// the status table uses: the change, the branch, the build's platform and
// verdict, the pull request, and any outcome that needs them. With -v it
// prints every state change with the job's recorded detail, as the driver
// sees it.
func (r *reporter) status(ctx context.Context, status workflow.Status) error {
	for _, entry := range status.Jobs {
		var err error
		if r.level >= progress.Verbose {
			err = r.detailed(entry)
		} else {
			err = r.narrate(entry, status.PullRequests)
		}
		if err != nil {
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

// detailed is the -v stream: the job's state with its recorded detail,
// reuse decisions, attempt errors, and admission waits, whenever any changes.
func (r *reporter) detailed(entry view.JobStatus) error {
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
	return r.changed("job:"+string(entry.Job.ID), message)
}

// narrate is the info-level story of one job: each line appears once, when
// the milestone it names is reached, and says only what a person waits for.
func (r *reporter) narrate(entry view.JobStatus, pulls []record.PullRequest) error {
	job := entry.Job
	id := string(job.ID)
	label := r.label(job)
	say := func(key, text string) error { return r.changed(key+":"+id, label+": "+text) }
	if job.Spec.Action.Prepares() && !(job.Spec.Action == record.Bump && job.ResolvedRelease == nil) {
		if err := say("change", view.ChangeWords(job)); err != nil {
			return err
		}
	}
	if job.Prepared != nil && job.ResultRevision != "" {
		if err := say("branch", "branch "+job.Prepared.Branch+" prepared"); err != nil {
			return err
		}
	}
	pr := pullRequestOf(entry, pulls)
	if reused := entry.Reused; reused != nil && reused.Evidence != nil && reused.Evidence.Verdict == record.VerdictPassed {
		if err := say("reused", "passed on "+platformLabel(reused.Spec.Config.Platform)+" (reused from an earlier build)"+advisoryTestNote(reused.Evidence)); err != nil {
			return err
		}
	}
	for _, attempt := range entry.Attempts {
		// A running attempt carries an observation whose verdict is unknown;
		// only a conclusion is a milestone.
		if attempt.Evidence == nil || attempt.Evidence.Verdict == "" || attempt.Evidence.Verdict == record.VerdictUnknown {
			continue
		}
		if err := say("verdict:"+string(attempt.ID), attemptLine(job, attempt)+advisoryTestNote(attempt.Evidence)); err != nil {
			return err
		}
	}
	if job.State == record.JobActive {
		switch state := view.JobState(entry, pr); state {
		case "preparing", "integrating branch", "verifying", "in progress":
			// Covered by the change, branch, and verdict lines.
		default:
			if err := say("state", state); err != nil {
				return err
			}
		}
	}
	if line := pullRequestLine(entry, pulls); line != "" {
		if err := say("pr", line); err != nil {
			return err
		}
	}
	switch job.State {
	case record.JobCompleted:
		// The verdict and PR lines already said it; only other outcomes need words.
		if outcome := completedOutcome(entry); outcome != "" && outcome != "verification passed" && outcome != "verification passed (reused)" && outcome != "publication confirmed" {
			return say("outcome", outcome)
		}
	case record.JobFailed, record.JobNeedsAttention, record.JobCanceled, record.JobSuperseded:
		text := view.JobState(entry, pr)
		if job.Detail != "" {
			text += "; " + job.Detail
		}
		return say("outcome", text)
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
		if entry.Job.Spec.Verification == record.VerificationSkipped {
			return "publication confirmed; verification skipped at the author's request"
		}
		return "publication confirmed"
	}
	if entry.Reused != nil && entry.Reused.Evidence != nil && entry.Reused.Evidence.Verdict == record.VerdictPassed {
		return verificationOutcome(entry.Job) + " (reused)" + advisoryTestNote(entry.Reused.Evidence)
	}
	if len(entry.Attempts) == 0 {
		return ""
	}
	var note string
	for _, attempt := range entry.Attempts {
		if attempt.State != record.AttemptFinished || attempt.Evidence == nil || attempt.Evidence.Verdict != record.VerdictPassed {
			return ""
		}
		if note == "" {
			note = advisoryTestNote(attempt.Evidence)
		}
	}
	return verificationOutcome(entry.Job) + note
}

// advisoryTestNote says when a passing build's declared tests failed, which
// the declared policy records without changing the verdict.
func advisoryTestNote(evidence *record.Evidence) string {
	if evidence == nil || evidence.TestFailure == "" {
		return ""
	}
	return "; the port's tests failed (advisory): " + evidence.TestFailure
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
