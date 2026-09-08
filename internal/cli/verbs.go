package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/report"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// verifyCmd is `verify <branch|port|subport|portdir>`: ONE ROAD, THREE
// SUBJECTS. A change that exists; a branch or a working tree with no
// record, which it ADOPTS; a branch a person continued past its record,
// which it FOLLOWS. Section 8 explicitly rejected splitting it — they
// are one operation with two subjects, and two verbs would make a user
// choose a spelling for a distinction the program can see.
//
// --on IS A SLICE HERE and singular on the intent verbs, and this is the
// one verb whose own flag asks for several releases at once.
func verifyCmd(s *Services) *cobra.Command {
	var (
		on            []string
		test, keepEnv bool
		trace         bool
		wait          time.Duration
	)
	c := &cobra.Command{
		Use:   "verify <branch|port|subport|portdir>",
		Short: "Verify a branch's tip in a pristine VM — or a port as it sits",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := s.Acquire(ctx, app.Needs{Repo: true, Verifier: true}); err != nil {
				return err
			}
			provisioned, err := provisionedReleases(ctx, s)
			if err != nil {
				return err
			}
			releases, err := resolveReleaseSet(on, provisioned, true)
			if err != nil {
				return err
			}
			if trace && len(releases) > 1 {
				// --trace streams ONE log, and a matrix has several. Refused
				// rather than picking one, because which of three builds a
				// person meant to watch is not a question this layer may answer
				// for them.
				return usagef("--trace follows one build; name one release with --on")
			}
			repo, err := s.Repo()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			led, err := s.Ledger()
			if err != nil {
				return err
			}
			residency := probeResidency(ctx, repo)
			// --trace IMPLIES --wait: streaming a log until it finishes is
			// staying for the answer, and a caller that streamed and then
			// detached would leave mid-sentence.
			stay := waitPtr(cmd, wait, trace)
			op := app.Verify{
				Repo: repo, Ledger: led, State: st,
				Stage:     &stager{repo: repo, temp: s.Temp(), session: s.session, release: releases[0]},
				Local:     s.ProposeTree(),
				Verifier:  s.VerifyProvider(),
				Me:        s.Me(s.Now()),
				Residency: residencyFunc(repo),
				Now:       s.Now,
				Progress:  sink{w: s.Err},
				Out:       s.Out,
			}
			res, err := op.Run(ctx, app.VerifyRequest{
				Target: args[0], Platforms: releases, Test: test, KeepEnv: keepEnv,
				Trace: trace, Wait: stay, Residency: residency,
			})
			report.VerifyRows(s.Out, res, residency)
			if err != nil {
				return err
			}
			return exitWith(res.Exit())
		},
	}
	c.Flags().StringSliceVar(&on, "on", nil,
		`macOS releases to verify on, or "all" (default: the newest provisioned base)`)
	c.Flags().BoolVar(&test, "test", false, "also run the port's test suite (port test) after the install")
	c.Flags().BoolVar(&keepEnv, "keep-env", false, "keep the environment after a pass, as a failure keeps its own")
	c.Flags().BoolVar(&trace, "trace", false, "stay attached after submitting: stream the build log until it finishes")
	c.Flags().DurationVar(&wait, "wait", 0, "stay through the build without showing the log")
	return c
}

// waitPtr is --wait as a request takes it, with --trace's implication
// applied. A pointer so "no wait" and "wait zero" stay two values.
func waitPtr(cmd *cobra.Command, d time.Duration, trace bool) *time.Duration {
	if cmd.Flags().Changed("wait") {
		v := d
		return &v
	}
	if trace {
		// An unbounded stay, spelled as a very long one: the caller asked to
		// watch until it finishes, and the context's own cancellation —
		// their Ctrl-C — is what ends it. Interrupting is safe, because the
		// build is detached and owned by the record.
		v := 24 * time.Hour
		return &v
	}
	return nil
}

// statusCmd is ONE VERB AT THREE DEPTHS, and the middle one is the
// default. The cut is local-against-remote and not
// reading-against-observing: the local half is bounded by running runs
// and the remote half scales with branches, with nothing bounding that.
//
//	--no-update  the pure read: the ledger as written. Polls nothing,
//	             writes nothing, TAKES NO LOCK, asks no forge and no
//	             provider — so the report says so on its first line
//	             rather than letting a missing line read as an answer.
//	(default)    poll the machine, SETTLE what is finished, read the
//	             forge FROM CACHE with its AsOf printed.
//	--refresh    the default plus a live forge ask, recorded into the
//	             cache.
//
// IT SETTLES WHAT IS RUNNING; ONLY A PASS STARTS WHAT IS QUEUED. That
// line is the whole of the no-dispatcher road, and it costs two things
// worth saying under a verb with this name: a person who never types it
// never settles anything, and settling RELEASES LEASES, so somebody
// typing `status` to look at something is also destroying VMs. What it
// must not also do is drain — a reporting verb that boots VMs is a
// different thing again.
func statusCmd(s *Services) *cobra.Command {
	var noUpdate, refresh, asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Report every dockhand change and its verification standing",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if noUpdate && refresh {
				return usagef("--no-update reads what is written and --refresh asks the forge; ask for one")
			}
			// THE DEPTH DECIDES THE DEPENDENCIES. --no-update opens a
			// repository and nothing else: no provider is resolved, no
			// evaluator is started, no forge runner is used. That is the
			// done-criterion in one line.
			needs := app.Needs{Repo: true}
			if !noUpdate {
				needs.Verifier, needs.Forge = true, true
			}
			if err := s.Acquire(ctx, needs); err != nil {
				return err
			}
			repo, err := s.Repo()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			led, err := s.Ledger()
			if err != nil {
				return err
			}
			// --no-update TAKES NO LOCK AT ALL, so its residency is Unknown
			// BY CONSTRUCTION rather than by a failed probe: it settles
			// nothing, and a report that claimed to know whether a scheduler
			// was up would be claiming a fact it deliberately did not ask
			// for.
			residency := app.Residency{State: app.ResidencyUnknown}
			if !noUpdate {
				residency = probeResidency(ctx, repo)
			}
			op := app.Status{
				Repo: repo, Ledger: led, State: st, Env: s.PublishEnv(),
				Local: s.ProposeTree(), Verifier: s.VerifyProvider(),
				Me: s.Me(s.Now()), Residency: residency, Now: s.Now,
			}
			res, runErr := op.Run(ctx, app.StatusRequest{NoUpdate: noUpdate, Forge: forgePolicy(noUpdate, refresh)})
			if asJSON {
				if err := emitJSON(s, statusDoc(res, runErr)); err != nil {
					return err
				}
				return runErr
			}
			if runErr == nil || len(res.State.Changes) > 0 {
				// A read that failed outright carries no standings, and a
				// report of a zero state would print a residency line and a
				// blank table under a sentence the caller is about to be
				// shown anyway. On a checkout dockhand has never run in that
				// sentence IS the report.
				report.Standings(s.Out, res, s.Now())
			}
			// Status's exit is NEVER 84 (that is a pass's own code): 0
			// unless it could not read. Status reports; the person acts.
			return runErr
		},
	}
	c.Flags().BoolVar(&noUpdate, "no-update", false, "the pure read: the ledger as written, polling nothing and taking no locks")
	c.Flags().BoolVar(&refresh, "refresh", false, "the default plus a live forge ask, recorded into the cache")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON on stdout")
	return c
}

// forgePolicy is the depth as publish takes it. `status` is
// ForgeAsCached and `--refresh` is ForgeRefresh; `--no-update` is
// ForgeAsCached with nothing recorded and nothing asked, which Gather
// reports as Fresh false and a zero AsOf.
//
// THE BOUNDARY THAT MUST NOT BE CROSSED: a cached standing serves the
// REPORT and never a DECISION — publish.Authorize refuses Facts whose
// Forge.Fresh is false, so nothing here can put a stale answer in front
// of a gate.
func forgePolicy(noUpdate, refresh bool) publish.ForgePolicy {
	if refresh && !noUpdate {
		return publish.ForgeRefresh
	}
	return publish.ForgeAsCached
}

// statusDocument is `status --json`: THE SAME REPORT, machine-readable,
// with the exit twin inside it so a caller that captured stdout through
// a pipe and lost $? still knows how the run ended.
//
// "The same report" is the whole specification and it is a demanding
// one. An earlier shape carried five fields per change — id, branch,
// state, tip, held — and record.ChangeState has no verdict values, so a
// passed change, a failed one, three still queued and one nobody ever
// asked to build all came back `"state":"minted"`: a dashboard over
// twelve changes could not find the failure, and reported the fleet
// healthy. Everything the human report renders is therefore here — the
// attempts and their per-member verdicts, the pull request with the
// AsOf that makes "open" different from "open when I last looked", the
// hold's reason, the proposals awaiting an answer, and the refs a
// foreign hand moved.
//
// WHAT IT CARRIES IS TYPED VALUES AND NEVER THE REPORT'S SENTENCES.
// report.Standings decides what a PERSON is told — "FAILED 2h ago",
// worst-run-first, in attention order — and that judgment is not
// duplicated here: a machine gets record.RunState per member and
// decides for itself, which is rule 6 and which is also why this
// document cannot drift from the report's prose. Every field is tagged,
// including the ones that arrive from another package's struct: an
// untagged embed spells its keys in Go's PascalCase, and a consumer
// reading `.vacancy.free` off a key named `Free` reads null.
type statusDocument struct {
	Exit        exitcode.Twin     `json:"exit"`
	Residency   residencyDoc      `json:"residency"`
	Settled     []string          `json:"settled,omitempty"`
	Vacancy     vacancyDoc        `json:"vacancy"`
	Changes     []changeDoc       `json:"changes"`
	Disagreeing []disagreementDoc `json:"disagreeing,omitempty"`
	Obligations []obligationDoc   `json:"obligations,omitempty"`
}

type residencyDoc struct {
	State  string    `json:"state"`
	Holder int       `json:"holder_pid,omitempty"`
	Since  time.Time `json:"since,omitzero"`
}

// vacancyDoc is verify.Vacancy with the tags it does not carry. Known
// is first and it is not omitted: "the machine has no free slots" and
// "I could not find out" are two facts (rule 7), and a document that
// dropped the flag would leave a zero Free meaning both.
type vacancyDoc struct {
	Known bool      `json:"known"`
	Free  int       `json:"free"`
	Limit int       `json:"limit"`
	AsOf  time.Time `json:"as_of,omitzero"`
}

// changeDoc is one change as a machine reads it: the record's own
// fields, then the three things the human report adds beneath the line
// — the verification standing, the pull request, and what the change
// proposes that nobody has answered.
type changeDoc struct {
	ID         record.ChangeID    `json:"id"`
	Branch     string             `json:"branch,omitempty"`
	State      record.ChangeState `json:"state"`
	Tip        string             `json:"tip,omitempty"`
	Held       bool               `json:"held"`
	HoldReason string             `json:"hold_reason,omitempty"`
	// Attempts are every attempt on this change, current or not. An
	// attempt whose Sha is not the change's tip says nothing about what
	// stands — which is what Current is for — but it is not dropped: a
	// caller asking why a branch was re-verified needs the former tip's
	// work to still be in the document.
	Attempts []attemptDoc    `json:"attempts"`
	PR       *pullRequestDoc `json:"pull_request,omitempty"`
	Proposes []string        `json:"proposes,omitempty"`
}

// attemptDoc is one attempt's standing. It carries the record's phase
// AND the three predicates the record exports over it, because they are
// not the same question: Queued is Requested with no lease, and a
// consumer re-deriving that from `phase` alone would be reimplementing
// record.Attempt.Queued in jq.
type attemptDoc struct {
	ID       string       `json:"id"`
	Platform string       `json:"platform,omitempty"`
	Sha      string       `json:"sha,omitempty"`
	Current  bool         `json:"current"`
	Phase    record.Phase `json:"phase"`
	Queued   bool         `json:"queued"`
	Active   bool         `json:"active"`
	Settled  bool         `json:"settled"`
	Started  time.Time    `json:"started,omitzero"`
	Runs     []runDoc     `json:"runs,omitempty"`
}

// runDoc is one member's verdict on one attempt — the value that was
// missing, and the only thing in this document that answers "did it
// build".
type runDoc struct {
	Port   string          `json:"port"`
	State  record.RunState `json:"state"`
	Detail string          `json:"detail,omitempty"`
}

// pullRequestDoc is a forge standing WITH THE MOMENT IT WAS LEARNED.
// Fresh says whether this invocation asked or served what a cycle
// cached, because "open" and "open when I last looked" are different
// claims and a machine must be able to tell them apart — the same
// boundary publish.Authorize enforces on the deciding side.
type pullRequestDoc struct {
	Number int       `json:"number"`
	State  string    `json:"state,omitempty"`
	URL    string    `json:"url,omitempty"`
	AsOf   time.Time `json:"as_of,omitzero"`
	Fresh  bool      `json:"fresh"`
}

// disagreementDoc is a bound record whose ref a foreign hand moved.
// Shown rather than skipped for the reason the report shows it: a
// change missing from the listing reads as nothing to report.
type disagreementDoc struct {
	ID       record.ChangeID `json:"id"`
	Ref      string          `json:"ref"`
	Recorded string          `json:"recorded,omitempty"`
	Found    string          `json:"found,omitempty"`
	Absent   bool            `json:"absent"`
}

// obligationDoc is one environment this checkout owes or merely found.
//
// It is an OBJECT and not a lease id, and that is the whole of a defect
// this shape removes: the document used to emit `o.ID.ID`, and an
// Untracked obligation is built from provider inventory and never has a
// lease — so two leaking guests serialized as `["", ""]`, two entries
// that could not be told apart, correlated with nothing, and passable
// to no follow-up command. Its identity lives in Worker, Request and
// Job, so those are carried; so is Root, because a foreign obligation is
// NAMED and never seized, and a machine that cannot read the name has
// no more idea whose it is than a person told "somebody else's".
type obligationDoc struct {
	Kind     string          `json:"kind"`
	Standing string          `json:"standing"`
	Seizable bool            `json:"seizable"`
	Change   record.ChangeID `json:"change,omitempty"`
	Platform string          `json:"platform,omitempty"`
	Worker   string          `json:"worker,omitempty"`
	Lease    string          `json:"lease,omitempty"`
	Request  string          `json:"request,omitempty"`
	Job      string          `json:"job,omitempty"`
	Root     string          `json:"root,omitempty"`
	Since    time.Time       `json:"since,omitzero"`
	Attempts int             `json:"attempts,omitempty"`
	Why      string          `json:"why,omitempty"`
}

func statusDoc(res app.StatusResult, err error) statusDocument {
	doc := statusDocument{
		Exit:      TwinOf(err),
		Residency: residencyDoc{State: residencyWord(res.Residency.State), Holder: res.Residency.Holder.PID, Since: res.Residency.Since},
		Settled:   res.Settled,
		Vacancy: vacancyDoc{
			Known: res.Vacancy.Known, Free: res.Vacancy.Free,
			Limit: res.Vacancy.Limit, AsOf: res.Vacancy.AsOf,
		},
	}
	// An empty listing is [] and never null. `changes` carries no
	// omitempty precisely so a checkout with nothing standing SAYS so,
	// and a consumer iterating a null it did not expect is a consumer
	// that crashes on the quietest possible answer.
	doc.Changes = []changeDoc{}
	byChange := map[record.ChangeID][]record.Attempt{}
	for _, a := range res.State.Attempts {
		byChange[a.Change] = append(byChange[a.Change], a)
	}
	for _, key := range slices.Sorted(maps.Keys(res.State.Changes)) {
		doc.Changes = append(doc.Changes, changeDocOf(res, res.State.Changes[key], byChange))
	}
	for _, d := range res.Disagreeing {
		doc.Disagreeing = append(doc.Disagreeing, disagreementDoc{
			ID: d.ID, Ref: d.Ref, Recorded: d.Recorded, Found: d.Found, Absent: d.Absent,
		})
	}
	for _, o := range res.Obligations {
		doc.Obligations = append(doc.Obligations, obligationDoc{
			Kind: obligationWord(o.Kind), Standing: standingWord(o.Standing), Seizable: o.Standing.Seizable(),
			Change: o.Change, Platform: o.Platform, Worker: o.Worker, Lease: o.ID.ID,
			Request: o.Request, Job: o.Job.ID, Root: o.Root, Since: o.Since,
			Attempts: o.Attempts, Why: o.Why,
		})
	}
	return doc
}

// changeDocOf is one change and everything hanging off it.
func changeDocOf(res app.StatusResult, c record.Change, byChange map[record.ChangeID][]record.Attempt) changeDoc {
	d := changeDoc{ID: c.ID, Branch: c.Branch, State: c.State, Tip: c.Tip, Held: c.Hold != nil}
	if c.Hold != nil {
		// "held" with no reason and "held for a reason nobody wrote down"
		// are the same to a reader, and the second is what happened.
		d.HoldReason = c.Hold.Reason
	}
	d.Attempts = []attemptDoc{}
	for _, a := range byChange[c.ID] {
		d.Attempts = append(d.Attempts, attemptDocOf(a, c.Tip))
	}
	slices.SortStableFunc(d.Attempts, func(x, y attemptDoc) int { return strings.Compare(x.ID, y.ID) })
	if f, ok := res.Facts[c.ID]; ok && f.Forge.OwnFound {
		d.PR = &pullRequestDoc{
			Number: f.Forge.Own.Number, State: f.Forge.Own.State, URL: f.Forge.Own.HTMLURL,
			AsOf: f.Forge.AsOf, Fresh: f.Forge.Fresh,
		}
	}
	for _, f := range c.Findings {
		if f.Disposition == record.Proposed {
			d.Proposes = append(d.Proposes, f.Criterion)
		}
	}
	return d
}

// attemptDocOf is one attempt, with its members' verdicts in port order
// so two runs of `status --json` over one store cannot differ.
func attemptDocOf(a record.Attempt, tip string) attemptDoc {
	d := attemptDoc{
		ID: a.ID, Platform: a.Platform, Sha: a.Sha, Current: a.Sha == tip,
		Phase: a.Phase, Queued: a.Queued(), Active: a.Active(), Settled: a.Settled(),
		Started: a.Started,
	}
	for _, port := range slices.Sorted(maps.Keys(a.Runs)) {
		r := a.Runs[port]
		d.Runs = append(d.Runs, runDoc{Port: port, State: r.State, Detail: r.Detail})
	}
	return d
}

// residencyWord is the three states as a document spells them. A word
// and not a number, because a JSON consumer reading `2` would have to
// carry this package's iota order.
func residencyWord(st app.ResidencyState) string {
	switch st {
	case app.NoDispatcher:
		return "none"
	case app.DispatcherResident:
		return "resident"
	case app.ResidencyUnknown:
	}
	return "unknown"
}

// obligationWord is an obligation's kind as a document spells it, for
// residencyWord's reason: lease.ObligationKind is an iota, and a
// consumer reading `3` would be carrying that package's declaration
// order. The zero value is named rather than dropped — an obligation of
// no stated kind is a fact Discharge refuses to act on, and a document
// that omitted it would read as no obligation at all.
func obligationWord(k lease.ObligationKind) string {
	switch k {
	case lease.Owed:
		return "owed"
	case lease.Requested:
		return "requested"
	case lease.Untracked:
		return "untracked"
	case lease.Due:
		return "due"
	case lease.UnknownObligation:
	}
	return "unknown"
}

// standingWord is whether THIS pass may act on an obligation, spelled
// for a machine. It rides beside Seizable rather than instead of it:
// the standing says who the environment belongs to and the boolean says
// what this pass would do about it, and a consumer paging an operator
// needs the first while a consumer predicting the next cycle needs the
// second.
func standingWord(st lease.Standing) string {
	switch st {
	case lease.Mine:
		return "mine"
	case lease.EarlierPass:
		return "earlier-pass"
	case lease.LiveElsewhere:
		return "live-elsewhere"
	case lease.DeadElsewhere:
		return "dead-elsewhere"
	case lease.ForeignRoot:
		return "foreign-root"
	case lease.Unattributed:
		return "unattributed"
	case lease.StandingUnknown:
	}
	return "unknown"
}

func emitJSON(s *Services, v any) error {
	enc := json.NewEncoder(s.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// cancelCmd stops a running verification, releasing its worker. It is
// the only one of the stopping verbs that spans more than one lifecycle
// — it stops a job, releases a lease and settles an attempt — which is
// why it is an app operation where the others call one mutator.
func cancelCmd(s *Services) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <branch|port>",
		Short: "Stop a running verification, releasing its worker",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := s.Acquire(ctx, app.Needs{Repo: true, Verifier: true}); err != nil {
				return err
			}
			repo, err := s.Repo()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			led, err := s.Ledger()
			if err != nil {
				return err
			}
			op := app.Cancel{
				Repo: repo, Ledger: led, State: st, Verifier: s.VerifyProvider(),
				Local: s.ProposeTree(), Me: s.Me(s.Now()), Now: s.Now, Progress: sink{w: s.Err},
			}
			res, err := op.Run(ctx, args[0])
			for _, id := range res.Stopped {
				fmt.Fprintf(s.Out, "stopped attempt %s\n", id)
			}
			for _, l := range res.Released {
				fmt.Fprintf(s.Out, "released environment %s\n", l)
			}
			return err
		},
	}
}

// dismissCmd records that a person looked at a change's PROPOSALS and
// said no. It answers the findings and leaves the branch alone.
//
// THE TOOL DECLINES; THE PERSON DISMISSES — two words for two
// directions, which is why this was not renamed to `decline`. A
// dismissal is RECORDED rather than the finding deleted: a finding that
// vanished when declined would be proposed again on the next look, and
// the ABI measurement does not change because somebody disagreed about
// acting on it. Under a resident dispatcher that is not a nicety — it is
// the only thing that can quiet a channel repeating a stable measurement
// 288 times a day.
func dismissCmd(s *Services) *cobra.Command {
	return &cobra.Command{
		Use:   "dismiss <branch|port>",
		Short: "Record that a person looked at a change's proposals and said no",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := s.Acquire(ctx, app.Needs{Repo: true}); err != nil {
				return err
			}
			repo, err := s.Repo()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			ref, err := change.Resolve(ctx, repo, st, args[0])
			if err != nil {
				return err
			}
			state, err := st.Read(ctx)
			if err != nil {
				return err
			}
			c := state.Changes[string(ref.ID())]
			f, ok := proposed(c)
			if !ok {
				return change.ErrNoProposal
			}
			if err := (app.Dismiss{State: st, Now: s.Now}).Run(ctx, c.ID, f.Kind, record.Dismissed, f.Candidates); err != nil {
				return err
			}
			fmt.Fprintf(s.Out, "dismissed the %s finding on %s: recorded, not deleted — the measurement stands\n", f.Kind, ref.Branch())
			return nil
		},
	}
}

// proposed is the one finding a dismissal answers: the change's own
// Proposed finding. A change carrying none is change.ErrNoProposal,
// which is the same refusal the cohort road raises for the same fact.
func proposed(c record.Change) (record.Finding, bool) {
	for _, f := range c.Findings {
		if f.Disposition == record.Proposed {
			return f, true
		}
	}
	return record.Finding{}, false
}

// holdCmd stops a change from publishing, verifying or being cleaned. A
// hold is the first refusal on every publication road, and also — not by
// coincidence — exactly how the machine road's prerelease exclusion is
// enforced: any prerelease target is BORN HELD.
func holdCmd(s *Services) *cobra.Command {
	var message string
	c := &cobra.Command{
		Use:   "hold <branch|port>",
		Short: "Stop a change from publishing, verifying or being cleaned",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := resolveChange(ctx, s, args[0])
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			if err := (app.Hold{State: st, Now: s.Now}).Run(ctx, id, message, s.Me(s.Now())); err != nil {
				return err
			}
			fmt.Fprintf(s.Out, "held %s\n", args[0])
			return nil
		},
	}
	c.Flags().StringVarP(&message, "message", "m", "", "why this is held")
	return c
}

// unholdCmd releases a held change so it can proceed again. It refuses a
// change nothing is holding rather than succeeding quietly: the verb was
// asked to release something and there was nothing to release.
func unholdCmd(s *Services) *cobra.Command {
	return &cobra.Command{
		Use:   "unhold <branch|port>",
		Short: "Release a held change so it can proceed again",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := resolveChange(ctx, s, args[0])
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			if err := (app.Unhold{State: st, Now: s.Now}).Run(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(s.Out, "released %s\n", args[0])
			return nil
		},
	}
}

// resolveChange is the shared front half of the one-mutator entry
// points: acquire a repository, resolve the target, hand back the id.
// They declare Needs{Repo} and nothing else, which is the whole reason
// they are separate from the operations — a hold opens no provider, no
// evaluator and no forge.
func resolveChange(ctx context.Context, s *Services, target string) (record.ChangeID, error) {
	if err := s.Acquire(ctx, app.Needs{Repo: true}); err != nil {
		return "", err
	}
	repo, err := s.Repo()
	if err != nil {
		return "", err
	}
	st, err := s.State()
	if err != nil {
		return "", err
	}
	ref, err := change.Resolve(ctx, repo, st, target)
	if err != nil {
		return "", err
	}
	return ref.ID(), nil
}

// discardCmd deletes an in-flight branch, releasing everything it holds.
//
// A HUMAN DISCARD OF A CHANGE WITH AN OPEN PUBLICATION IS REFUSED, with
// a remedy that names the real off switch: close the pull request, and
// the next pass retires it. A change with an open pull request does not
// die by a local verb.
func discardCmd(s *Services) *cobra.Command {
	return &cobra.Command{
		Use:   "discard <branch|port>",
		Short: "Delete an in-flight branch, releasing everything it holds",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := s.Acquire(ctx, app.Needs{Repo: true, Verifier: true}); err != nil {
				return err
			}
			repo, err := s.Repo()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			led, err := s.Ledger()
			if err != nil {
				return err
			}
			op := app.Discard{
				Repo: repo, State: st, Ledger: led, Verifier: s.VerifyProvider(),
				Local: s.ProposeTree(), Me: s.Me(s.Now()), Now: s.Now,
				Progress: sink{w: s.Err},
				// Invoker is a CONSTANT of this road: a typed verb is a person,
				// and there is nothing to detect.
				Invoker: record.Human,
			}
			res, err := op.Run(ctx, args[0])
			if res.Closed != "" {
				fmt.Fprintf(s.Out, "discarded %s\n", res.Closed)
			}
			for _, ref := range res.Deleted {
				fmt.Fprintf(s.Out, "  deleted %s\n", ref)
			}
			return err
		},
	}
}

// promoteCmd pushes a verified branch to the fork and opens the pull
// request.
//
// IT IS THE PERSON'S ROAD BY CONSTRUCTION. With --auto retired the
// invoker is no longer something an invocation carries, so a typed
// `promote` is a person and there is nothing here to check —
// PromoteIsHumanError is deleted rather than moved. It refuses nothing
// on version grounds either: a person may publish a change to anything
// they could type, including a prerelease, on "the operator typed it",
// and the crossing earns a WARNING here where it earns a hold on the
// machine's road.
//
// --closes IS GONE FROM HERE. The ticket is named once, when the change
// is made, and carried: `bump --closes` lands it on the mint commit's
// trailer and on the record, and the body reads it off the record — so a
// second flag at publication time was only a chance to say two different
// numbers about one change.
func promoteCmd(s *Services) *cobra.Command {
	var asks publish.Asks
	c := &cobra.Command{
		Use:   "promote <branch|port>",
		Short: "Push a verified branch to your fork and open the pull request",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// A publication reads the forge, moves a branch and compares two
			// evaluations of the port; it starts nothing, so NO VERIFIER IS
			// RESOLVED — which is what keeps `promote` working on a host with
			// no tart at all.
			if err := s.Acquire(ctx, app.Needs{Repo: true, Evaluator: true, Forge: true}); err != nil {
				return err
			}
			op, err := promoteOp(s)
			if err != nil {
				return err
			}
			res, err := op.Run(ctx, args[0], asks)
			report.Promotion(s.Out, res)
			return err
		},
	}
	c.Flags().StringVar(&asks.Remote, "remote", "", "the fork remote to push to (default: the remote owned by your gh login)")
	c.Flags().StringVar(&asks.Title, "title", "", "PR title (default: the tip commit's subject)")
	c.Flags().BoolVar(&asks.NoPR, "no-pr", false, "push to the fork without opening a pull request")
	c.Flags().BoolVar(&asks.Ignore, "ignore", false,
		"promote past a FAILED verification: the verdict is ignored and the body states it")
	c.Flags().BoolVar(&asks.NoPRCheck, "no-pr-check", false, "skip the duplicate-PR search by title convention")
	c.Flags().BoolVar(&asks.Force, "force", false,
		"force-push the fork with lease (NOT --replace, which demolishes a local branch)")
	c.Flags().BoolVar(&asks.Body, "body", false, "emit the PR body and do nothing else")
	return c
}

// promoteOp builds the operation both `promote` and the one permitted
// line of sequencing use, so that what a pull request says is decided in
// one place.
//
// Grants.Invoker is record.Human as a CONSTANT of this road — that is
// what "human by construction" means, and it is why the zero Driver is a
// wiring gap rather than a person.
func promoteOp(s *Services) (app.Promote, error) {
	repo, err := s.Repo()
	if err != nil {
		return app.Promote{}, err
	}
	st, err := s.State()
	if err != nil {
		return app.Promote{}, err
	}
	led, err := s.Ledger()
	if err != nil {
		return app.Promote{}, err
	}
	return app.Promote{
		Repo: repo, Ledger: led, State: st, Env: s.PublishEnv(),
		Grants:    app.Grants{Invoker: record.Human, Grant: s.Grant},
		Residency: app.Residency{},
		Now:       s.Now, Progress: sink{w: s.Err},
	}, nil
}

// cohortMode is `bump-revision --for <branch>`: the PLURAL invocation,
// which accepts the revbump proposal a verification measured and
// revbumps its dependents as one more commit on the branch that already
// carries the change. It never mints — the members move for one reason
// and it is the same reason.
//
// It registers its flags and returns the reader of them, which answers
// whether this invocation is the plural one. The arity check calls that
// reader BEFORE it counts arguments, because `--for <branch>` names a
// branch and every member on it, and demanding a port would be demanding
// the answer the proposal already holds.
func cohortMode(c *cobra.Command, f *intentFlags) func() (bool, error) {
	c.Flags().StringVar(&f.forBranch, "for", "", "accept the revbump proposal on this branch")
	c.Flags().StringSliceVar(&f.exclude, "exclude", nil,
		"leave these members out of the change entirely: not bumped, not built, and listed so a reviewer can disagree")
	c.Flags().StringSliceVar(&f.forceWithheld, "force-withheld", nil,
		"build these withheld members anyway, LAST, with the member each conflicts with deactivated first")
	return func() (bool, error) {
		if f.forBranch == "" {
			if len(f.exclude) > 0 || len(f.forceWithheld) > 0 {
				return false, usagef("--exclude and --force-withheld need the --for that accepts a proposal")
			}
			return false, nil
		}
		switch {
		case f.planOnly || f.diff || f.inPlace:
			return false, usagef("--for grafts a commit onto an existing branch; it needs the default realization")
		case f.riders || f.noRiders:
			return false, usagef("a cohort commit revbumps other people's ports and makes no other edit")
		case f.replace:
			return false, usagef("--for extends a branch rather than minting one; --replace has nothing to replace")
		}
		for _, m := range f.exclude {
			if slices.Contains(f.forceWithheld, m) {
				return false, usagef("%s is named by both --exclude and --force-withheld: a member is left out or forced, not both", m)
			}
		}
		return true, nil
	}
}

// runAccept is the plural road's body: app.Accept over the branch the
// proposal is on.
func runAccept(ctx context.Context, s *Services, f *intentFlags) error {
	if err := f.check(); err != nil {
		return err
	}
	if err := s.Acquire(ctx, app.Needs{Repo: true, Evaluator: true, Verifier: !f.noVerify}); err != nil {
		return err
	}
	repo, err := s.Repo()
	if err != nil {
		return err
	}
	st, err := s.State()
	if err != nil {
		return err
	}
	led, err := s.Ledger()
	if err != nil {
		return err
	}
	residency := probeResidency(ctx, repo)
	op := app.Accept{
		Plan: planningFor(s), Repo: repo, Ledger: led, State: st,
		Stage:     &stager{repo: repo, temp: s.Temp(), session: s.session, release: f.release},
		Local:     s.ProposeTree(),
		Verifier:  s.VerifyProvider(),
		Me:        s.Me(s.Now()),
		Residency: residencyFunc(repo),
		Now:       s.Now,
		Progress:  sink{w: s.Err},
		Prepare:   cohortPrepare(s),
	}
	res, err := op.Run(ctx, app.AcceptRequest{
		Branch: f.forBranch, Exclude: f.exclude, ForceWithheld: f.forceWithheld,
		NoVerify: f.noVerify, Test: f.test, KeepEnv: f.keepEnv, Platform: f.release,
		Wait: f.waitFor(), Residency: residency,
		Prov: change.Provenance{AskedBy: record.Human, Via: record.MintedCohort, Agent: s.Agent},
	})
	report.Change(s.Out, quietWhereNoBuildWasAsked(res, f.delivery()), residency)
	if err != nil {
		return err
	}
	return exitWith(res.Exit())
}

// logCmd prints the build log out of a target's verification
// environment, as it stands right now — mid-build for a running job,
// complete for a kept failure.
//
// --trace IS THE ONLY WAY TO WATCH A BUILD HAPPEN, and it always was:
// --trace left the change verbs precisely because this flag already did
// the thing, so no capability moved when it went. THE LOG IS THE
// PROVIDER'S AND THE VERDICT IS THE RECORD'S: streaming output is not
// judging, and this verb never takes the second step. With a dispatcher
// resident that dispatcher judges and this is a spectator; with none,
// the process that is waiting is the judge.
func logCmd(s *Services) *cobra.Command {
	var on string
	var trace, errs bool
	c := &cobra.Command{
		Use:   "log <branch|port|worker>",
		Short: "Print the build log from a verification environment",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := s.Acquire(ctx, app.Needs{Repo: true, Verifier: true}); err != nil {
				return err
			}
			prov, job, err := reachable(ctx, s, args[0], on)
			if err != nil {
				return err
			}
			switch {
			case trace:
				st, ok := prov.(verify.Streamer)
				if !ok {
					return fmt.Errorf("%w: this provider does not stream a live log", verify.ErrUnsupported)
				}
				return st.Stream(ctx, job, s.Out)
			case errs:
				log, err := prov.Log(ctx, job)
				if err != nil {
					return err
				}
				fmt.Fprint(s.Out, errorLines(log))
				return nil
			}
			log, err := prov.Log(ctx, job)
			if err != nil {
				return err
			}
			if log == "" {
				fmt.Fprintln(s.Err, "no log output yet")
				return nil
			}
			fmt.Fprint(s.Out, log)
			return nil
		},
	}
	c.Flags().StringVar(&on, "on", "", "which platform's environment, when several are reachable")
	c.Flags().BoolVar(&trace, "trace", false, "stream the log as it is written, until the build finishes")
	c.Flags().BoolVar(&errs, "errors", false, "dig the :error: lines and their context out of the guest's main.log")
	return c
}

// errorLines digs the :error: lines and the context around them out of a
// guest's log. Three lines of context on either side, which is what a
// configure failure needs to be readable and what a compile failure's
// preceding warning fits into.
func errorLines(log string) string {
	lines := strings.Split(log, "\n")
	keep := make([]bool, len(lines))
	found := false
	for i, l := range lines {
		if !strings.Contains(l, ":error:") {
			continue
		}
		found = true
		for j := max(0, i-3); j < min(len(lines), i+4); j++ {
			keep[j] = true
		}
	}
	if !found {
		return "no :error: lines in this log\n"
	}
	var b strings.Builder
	for i, l := range lines {
		if keep[i] {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// shellCmd opens an interactive shell inside a verification environment,
// which is what a kept failure is for: the build's remains exactly as
// the guest left them.
func shellCmd(s *Services) *cobra.Command {
	var on string
	c := &cobra.Command{
		Use:   "shell <branch|port|worker>",
		Short: "Open a shell inside a verification environment",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := s.Acquire(ctx, app.Needs{Repo: true, Verifier: true}); err != nil {
				return err
			}
			prov, job, err := reachable(ctx, s, args[0], on)
			if err != nil {
				return err
			}
			sh, ok := prov.(verify.InteractiveShell)
			if !ok {
				return fmt.Errorf("%w: this provider's environments do not take an interactive shell; `dockhand log` still reads their output", verify.ErrUnsupported)
			}
			fmt.Fprintf(s.Err, "connecting to %s\n", job.ID)
			return sh.Shell(ctx, job)
		},
	}
	c.Flags().StringVar(&on, "on", "", "which platform's environment, when several are reachable")
	return c
}

// reachable resolves a branch, a port or a worker name to ONE
// verification environment.
//
// A worker name addresses its environment directly, no change involved:
// the printed handle is all a person holds when a kept failure's change
// has been discarded. Otherwise the change is resolved, its LEASES are
// walked — a lease is where a provider's handle lives, and an attempt
// names one by token — and --on picks a platform where several answer.
// One candidate needs no picking, and anything else is refused with the
// choices named.
//
// It never boots anything. A released environment is gone and says so.
func reachable(ctx context.Context, s *Services, target, on string) (verify.Verifier, verify.Job, error) {
	prov, err := s.Verifier(ctx)
	if err != nil {
		return nil, verify.Job{}, err
	}
	if strings.HasPrefix(target, tart.WorkerPrefix) {
		return prov, verify.Job{Provider: "tart", ID: target}, nil
	}
	repo, err := s.Repo()
	if err != nil {
		return nil, verify.Job{}, err
	}
	st, err := s.State()
	if err != nil {
		return nil, verify.Job{}, err
	}
	ref, err := change.Resolve(ctx, repo, st, target)
	if err != nil {
		return nil, verify.Job{}, err
	}
	state, err := st.Read(ctx)
	if err != nil {
		return nil, verify.Job{}, err
	}
	byPlatform := map[string]record.Lease{}
	var plats []string
	for _, key := range slices.Sorted(maps.Keys(state.Leases)) {
		l := state.Leases[key]
		if l.Change != ref.ID() || !l.Held() {
			continue
		}
		if _, seen := byPlatform[l.Platform]; seen {
			continue
		}
		byPlatform[l.Platform] = l
		plats = append(plats, l.Platform)
	}
	switch {
	case len(plats) == 0:
		return nil, verify.Job{}, fmt.Errorf("%s: no environment to reach; `dockhand verify %s` starts one", target, target)
	case on != "":
		r, err := parseRelease(on)
		if err != nil {
			return nil, verify.Job{}, err
		}
		l, ok := byPlatform[r.Name]
		if !ok {
			return nil, verify.Job{}, fmt.Errorf("%s has no reachable environment on %s", target, r.Name)
		}
		return prov, jobOf(l), nil
	case len(plats) > 1:
		return nil, verify.Job{}, usagef("%s has environments on %s; pick one with --on", target, strings.Join(plats, ", "))
	}
	return prov, jobOf(byPlatform[plats[0]]), nil
}

// jobOf reconstructs the provider's address for a lease's environment.
// Request is left empty on purpose: it is not part of a job's identity —
// Release and Poll are addressed by Provider and ID — so a caller
// rebuilding a Job from a record loses nothing.
func jobOf(l record.Lease) verify.Job {
	return verify.Job{Provider: l.ID.Provider, ID: l.ID.ID, Started: l.ID.Started, Request: l.Request}
}

// execCmd runs one command on pristine clones of provisioned bases: the
// cheap question the verification pipeline is too heavy for. Field
// evidence made the case — bracketing which macOS releases carry a
// symbol took five hand-rolled clone/boot/probe/delete cycles at seconds
// each, against ten-minute builds to learn the same fact.
//
// IT IS THE ONE ROAD THAT RUNS WITH NO REPOSITORY AT ALL, which is the
// road record.OwnerID.Root exists to identify. Sequential on purpose:
// probes share the machine's guest cap with real verifications, and one
// slot briefly borrowed beats two occupied.
func execCmd(s *Services) *cobra.Command {
	var on []string
	c := &cobra.Command{
		Use:   "exec [--on <release>[,<release>]|--on all] -- <command> [args...]",
		Short: "Run a command on pristine clones of provisioned bases",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usagef("exec needs a command to run")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := s.Acquire(ctx, app.Needs{Verifier: true}); err != nil {
				return err
			}
			provisioned, err := provisionedReleases(ctx, s)
			if err != nil {
				return err
			}
			releases, err := resolveReleaseSet(on, provisioned, false)
			if err != nil {
				return err
			}
			var failed int
			for _, r := range releases {
				fmt.Fprintf(s.Err, "=== %s\n", r)
				out, err := tart.RunOnBase(ctx, s.Tools, tart.BaseName(r), args)
				if out != "" {
					fmt.Fprintln(s.Out, strings.TrimRight(out, "\n"))
				}
				if err != nil {
					// A FULL MACHINE IS NOT THIS COMMAND FAILING ON A RELEASE.
					// The probe never ran, nothing was queued for it, and the cap
					// is machine-wide — so every remaining release meets the same
					// wall — which makes counting it as a failure both a lie about
					// what happened and a waste of the boots that follow.
					if errors.Is(err, verify.ErrNoVacancy) {
						return err
					}
					failed++
					fmt.Fprintf(s.Err, "%s: %v\n", r.Name, err)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			if failed > 0 {
				return fmt.Errorf("exec: the command failed on %d of %d releases", failed, len(releases))
			}
			return nil
		},
	}
	c.Flags().StringSliceVar(&on, "on", nil,
		`macOS releases to probe, or "all" (default: the newest provisioned base)`)
	return c
}

// cohortPrepare is app.Accept's plan-and-prepare seam: the WHOLE
// cohort's content in one change.Prepared, planned from the TIP's blobs
// rather than from the working tree, so a cohort commit re-declares
// exactly what the branch already carries.
//
// IT IS A FUNCTION AND NOT A CALL INTO planning BECAUSE OF WHERE THE
// BLOBS COME FROM. Preparing a member needs the base commit's Portfile
// read out of git at the RESOLVED TIP, an evaluator to hold the
// prediction against, and the intent's parameters — and a blob read and
// an evaluation are exactly what "no operation opens a file" forbids
// app from doing.
//
// IT PREPARES ONE MEMBER, AND THAT IS A MEASURED LIMIT RATHER THAN A
// PREFERENCE. change.Prepared carries ONE Portdir and change's
// materialize joins every File.Path under it, so a Prepared per member
// cannot be concatenated — the second member's files would land under
// the first member's directory — and a cohort spanning several portdirs
// is not expressible in one Prepared as the type stands. app.Accept's
// own doc records the same finding and puts the whole cohort behind this
// one seam so the gap stays at one visible boundary; this implementation
// therefore refuses a multi-portdir cohort by name rather than writing a
// wrong join, and the remedy is a Prepared that carries per-subject
// portdirs.
func cohortPrepare(s *Services) func(context.Context, string, []record.Candidate) (change.Prepared, error) {
	return func(ctx context.Context, tip string, cands []record.Candidate) (change.Prepared, error) {
		dirs := portdirsOf(cands)
		switch len(dirs) {
		case 0:
			return change.Prepared{}, change.ErrEmptyCohort
		case 1:
		default:
			return change.Prepared{}, fmt.Errorf(
				"%w: this cohort spans %d portdirs and change.Prepared carries one; exclude members until it does",
				change.ErrEmptyCohort, len(dirs))
		}
		repo, err := s.Repo()
		if err != nil {
			return change.Prepared{}, err
		}
		ev, err := s.Eval()
		if err != nil {
			return change.Prepared{}, err
		}
		dir := dirs[0]
		blob, err := repo.BlobAt(ctx, tip, dir+"/Portfile")
		if err != nil {
			return change.Prepared{}, err
		}
		at, err := repo.CommittedAt(ctx, tip)
		if err != nil {
			return change.Prepared{}, err
		}
		pl, err := planningFor(s).Plan(ctx, "bump-revision", targetOf(dir, cands[0]), cohortParams(dir, cands))
		if err != nil {
			return change.Prepared{}, err
		}
		return change.Prepare(ctx, pl, change.Source{
			Base: record.Base{Sha: tip, CommittedAt: at}, Portdir: change.TreePath(dir), Portfile: blob,
		}, blobEvaluator{ev: ev})
	}
}

// portdirsOf is the distinct portdirs a candidate list touches, in
// first-seen order.
func portdirsOf(cands []record.Candidate) []string {
	var out []string
	for _, c := range cands {
		if c.Portdir != "" && !slices.Contains(out, c.Portdir) {
			out = append(out, c.Portdir)
		}
	}
	return out
}

// cohortParams is the parameters a cohort member is re-planned with: a
// revision bump for the reason the MEASUREMENT gave, which is why the
// plural road takes no --reason. Riders are RidersNone, because a cohort
// commit revbumps other people's ports and makes no other edit.
func cohortParams(dir string, cands []record.Candidate) intent.Params {
	return intent.Params{
		Target: dir,
		Reason: cohortReason(cands),
		Riders: intent.RidersNone,
	}
}

// cohortReason is the criterion the proposal rests on, as the reason a
// revbump commit states. It is the candidate's own words and never a
// paraphrase: the whole argument for a proposal is that a person can
// check the one claim behind it by hand, and a commit body, a pull
// request and a terminal line that each reworded it would be three
// claims a reviewer has to reconcile.
func cohortReason(cands []record.Candidate) string {
	for _, c := range cands {
		if c.Reason != "" {
			return c.Reason
		}
	}
	return "rebuild against the headline change"
}

// targetOf names the evaluation context a cohort member is.
func targetOf(dir string, c record.Candidate) tree.Target {
	t := tree.Target{Portdir: dir}
	if c.Port != "" && c.Port != pathBase(dir) {
		t.Subport = c.Port
	}
	return t
}

// pathBase is filepath-free basename over a tree-relative, slash-joined
// path: a record's Portdir is always slash-separated whatever the host.
func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// purgeCmd removes this checkout's dockhand branches, pins and records.
//
// It is a housekeeping verb and it sits beside discard, which is the
// same act over one change. What purge does NOT do is close anything:
// it removes the local git artifacts and leaves the state ref alone,
// so the change records survive and `status` still lists them. That
// asymmetry is app.Purge's, stated in its doc and reported on the last
// line of its own output, because a person whose branches have all just
// gone will read the next `status` as a bug otherwise.
//
// Needs is Repo alone: no verifier, no planner, no tree. Purge asks git
// what it holds and the store what it remembers, and a checkout with no
// MacPorts installation and no provider can still be cleaned up — which
// is very often exactly the checkout that needs it.
func purgeCmd(s *Services) *cobra.Command {
	var dry, force, envs bool
	c := &cobra.Command{
		Use:   "purge",
		Short: "Remove this checkout's dockhand branches, pins and records",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// Needs is decided by the flags, which is the composition
			// root's whole job: a purge without --environments resolves
			// no verifier at all, and on a host with no provider that is
			// the difference between working and refusing.
			if err := s.Acquire(ctx, app.Needs{Repo: true, Verifier: envs}); err != nil {
				return err
			}
			repo, err := s.Repo()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			led, err := s.Ledger()
			if err != nil {
				return err
			}
			op := app.Purge{
				Repo: repo, State: st, Ledger: led,
				Progress: sink{w: s.Err},
				DryRun:   dry, Force: force, Environments: envs,
			}
			if envs {
				op.Verifier = s.VerifyProvider()
			}
			res, err := op.Run(ctx)
			if err != nil {
				return err
			}
			report.Purged(s.Out, res)
			return nil
		},
	}
	c.Flags().BoolVar(&dry, "dry-run", false, "list what would be removed and remove nothing")
	c.Flags().BoolVar(&force, "force", false, "proceed even while an environment is held — the running build's branch goes with it")
	c.Flags().BoolVar(&envs, "environments", false, "also release every environment the provider is running; base and golden images are untouched")
	return c
}
