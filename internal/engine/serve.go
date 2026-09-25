package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// ServeOptions are what serve does besides running the queue, as the
// person's configuration and flags set them (Design v3 §11).
type ServeOptions struct {
	// Drain runs what is queued now and returns once the queue is empty.
	Drain bool
	// Capacity is how many checks each provider runs at once; one for a
	// provider it doesn't name.
	Capacity map[string]int
	// SubmitPassing opens a pull request for each branch serve prepared
	// whose check passed, at most SubmitLimit a day.
	SubmitPassing bool
	SubmitLimit   int
	// Outdated is the daily look for new releases of your ports.
	Outdated ServeOutdated
	// Cleanup turns automatic cleanup on (decision 36), removing what has
	// gone unused for CleanupAge.
	Cleanup    bool
	CleanupAge time.Duration
	// Say receives serve's lines, one at a time; Notify, what is worth a
	// notification. Either may be nil.
	Say    func(line string)
	Notify func(title, text string)
	// Poll, Refresh, and CleanupEvery pace serve: how often it looks for
	// work, reads pull requests, and cleans up. Zero means two seconds,
	// five minutes, and a day.
	Poll, Refresh, CleanupEvery time.Duration
	// Now is the clock serve's daily work reads; time.Now when nil.
	Now func() time.Time
}

// ServeOutdated is serve.for_outdated, and whose ports it looks at.
type ServeOutdated struct {
	// Maintainers are your identities; with none, serve says it needs them.
	Maintainers []string
	// Hour and Minute are when it looks each day.
	Hour, Minute int
	// Mode is list, which counts them for status; draft, which prepares a
	// branch for each; or check, which also checks each.
	Mode string
	// On and Tests are what check mode checks with.
	On    []string
	Tests model.TestPolicy
}

// ServeSubmitKind is the journal's kind for a pull request serve opened
// by itself; serve's daily limit counts them.
const ServeSubmitKind = "serve.submit"

// Serve leads the repository's queue for a session until ctx ends, or with
// Drain until the queue is empty, and does the rest of serve's work
// between checks (Design v3 §11). With another serve leading it stands by,
// and takes over when that one's session dies; a drain has nothing to do
// then. Runs still going when it stops are left for the next serve.
func (e *Engine) Serve(ctx context.Context, session *coord.Session, options ServeOptions) error {
	s := newServer(e, options)
	lease, err := s.lead(ctx, session)
	if err != nil || lease == nil {
		return err
	}
	defer session.Release(context.WithoutCancel(ctx), *lease)
	return s.run(ctx, session, *lease)
}

// server is one serve's state while it leads.
type server struct {
	e       *Engine
	options ServeOptions
	mu      sync.Mutex
}

func newServer(e *Engine, options ServeOptions) *server {
	if options.Poll <= 0 {
		options.Poll = 2 * time.Second
	}
	if options.Refresh <= 0 {
		options.Refresh = 5 * time.Minute
	}
	if options.CleanupEvery <= 0 {
		options.CleanupEvery = 24 * time.Hour
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &server{e: e, options: options}
}

// say writes one of serve's lines whole.
func (s *server) say(format string, args ...any) {
	if s.options.Say == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.options.Say(fmt.Sprintf(format, args...))
}

func (s *server) notify(title, text string) {
	if s.options.Notify != nil {
		s.options.Notify(title, text)
	}
}

// lead takes the lead, or stands by until the leader goes. A drain with
// another serve leading has nothing to do.
func (s *server) lead(ctx context.Context, session *coord.Session) (*model.Lease, error) {
	announced := false
	for {
		lease, err := session.Lead(ctx)
		if err == nil {
			return &lease, nil
		}
		held := new(coord.HeldError)
		if !errors.As(err, &held) {
			return nil, err
		}
		if s.options.Drain {
			s.say("serve (pid %d) leads and runs the queue; nothing to drain here", held.Holder.PID)
			return nil, nil
		}
		if !announced {
			s.say("serve: standing by; serve (pid %d) leads", held.Holder.PID)
			announced = true
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(s.options.Poll):
		}
	}
}

func (s *server) run(ctx context.Context, session *coord.Session, lease model.Lease) error {
	e, options := s.e, s.options
	var providers []string
	for name := range e.Providers {
		providers = append(providers, name)
	}
	slices.Sort(providers)
	capacity := map[string]int{}
	var described []string
	for _, name := range providers {
		capacity[name] = max(options.Capacity[name], 1)
		described = append(described, fmt.Sprintf("%s (%d at a time)", name, capacity[name]))
	}
	if len(described) == 0 {
		described = []string{"none set up; checks will need attention"}
	}
	publishing := "opens no pull requests; it only checks"
	if options.SubmitPassing {
		publishing = fmt.Sprintf("opens PRs for passing updates it prepared, at most %d a day", options.SubmitLimit)
	}
	s.say("serve: leading (pid %d) · builds on %s · %s", os.Getpid(), strings.Join(described, ", "), publishing)
	e.announceServing(options.SubmitPassing)
	followed := &follower{s: s, reported: map[string]bool{}}
	cleaned := &cleaner{s: s}
	scanned := &outdatedScanner{s: s}
	submitter := &passingSubmitter{s: s, held: map[model.BranchID]string{}}

	// Runs are driven concurrently, each provider up to its capacity. A
	// run takes a slot on every provider its plan builds on, for as long
	// as it runs; one that can't have them all waits, and the runs after
	// it that can go ahead.
	running, stop := context.WithCancel(ctx)
	defer stop()
	inUse := map[string]int{}
	inFlight := map[model.RunID]bool{}
	type finished struct {
		run       model.Run
		branch    model.Branch
		providers []string
		err       error
	}
	done := make(chan finished)
	var failure error
	launch := func(run model.Run, branch model.Branch, needs []string) {
		inFlight[run.ID] = true
		for _, name := range needs {
			inUse[name]++
		}
		verb := "running"
		if run.State == model.RunRunning {
			verb = "resuming"
		}
		s.say("%s %s: %s", run.Name(), branch.ShortName(), verb)
		go func() {
			result, err := e.Resume(running, session, run.ID)
			if result.ID == "" {
				result = run
			}
			done <- finished{run: result, branch: branch, providers: needs, err: err}
		}()
	}
	settle := func(f finished) {
		delete(inFlight, f.run.ID)
		for _, name := range f.providers {
			inUse[name]--
		}
		if held := new(coord.HeldError); errors.As(f.err, &held) {
			return
		}
		if f.err != nil {
			if failure == nil && running.Err() == nil {
				failure = f.err
				stop()
			}
			return
		}
		if !f.run.State.Terminal() {
			s.say("%s: left running for the next serve", f.run.Name())
			return
		}
		line := fmt.Sprintf("%s %s: %s", f.run.Name(), f.branch.ShortName(), f.run.State)
		if f.run.Detail != "" && f.run.State != model.RunPassed {
			line += ": " + f.run.Detail
		}
		s.say("%s", line)
		s.notify(f.branch.ShortName(), line)
		// A check that passed may be what the submitter waits for.
		submitter.last = time.Time{}
	}
	for running.Err() == nil {
		if !options.Drain {
			followed.maybe(running)
			scanned.maybe(running)
			if options.SubmitPassing {
				submitter.maybe(running)
			}
		}
		// A leader judged dead by a standby has lost the lease; it stops
		// rather than drive work twice.
		if err := session.Fenced(running, lease, func(store.Tx) error { return nil }); err != nil {
			if errors.Is(err, store.ErrStale) {
				failure = errors.New("serve: another serve took over the lead; stopping")
			} else if running.Err() == nil {
				failure = err
			}
			break
		}
		candidates, err := e.Candidates(running, session)
		if err != nil {
			if running.Err() == nil {
				failure = err
			}
			break
		}
		waiting := false
		for _, run := range candidates {
			if inFlight[run.ID] {
				continue
			}
			needs, err := e.RunProviders(running, run)
			if err != nil {
				failure = err
				break
			}
			if slices.ContainsFunc(needs, func(name string) bool { return inUse[name] >= max(capacity[name], 1) }) {
				waiting = true
				continue
			}
			branch, err := e.Branch(running, run.Branch)
			if err != nil {
				failure = err
				break
			}
			launch(run, branch, needs)
		}
		if failure != nil {
			break
		}
		if len(inFlight) == 0 && !waiting {
			// Cleanup waits for a quiet moment, so it never delays a check.
			if options.Drain {
				s.say("serve: the queue is empty")
				return nil
			}
			cleaned.maybe(running)
		}
		select {
		case <-running.Done():
		case f := <-done:
			settle(f)
		case <-time.After(options.Poll):
		}
	}
	// Runs still going stop with serve, and are left for the next one.
	stop()
	for len(inFlight) > 0 {
		settle(<-done)
	}
	if failure != nil {
		return failure
	}
	s.say("serve: stopped")
	return nil
}

// follower reads the pull requests every Refresh, reporting what changed,
// and each problem once until it changes.
type follower struct {
	s        *server
	last     time.Time
	reported map[string]bool
}

func (f *follower) maybe(ctx context.Context) {
	if !f.last.IsZero() && time.Since(f.last) < f.s.options.Refresh {
		return
	}
	f.last = time.Now()
	refreshed, err := f.s.e.RefreshPullRequests(ctx)
	problems := map[string]bool{}
	report := func(problem string) {
		problems[problem] = true
		if !f.reported[problem] {
			f.s.say("%s", problem)
		}
	}
	if err != nil {
		report(fmt.Sprintf("serve: could not read pull requests: %v", err))
	}
	for _, r := range refreshed {
		if r.Err != nil {
			report(fmt.Sprintf("serve: could not read %s's #%d: %v", r.Branch.ShortName(), r.Branch.PullRequest.Number, r.Err))
		}
		for _, change := range r.Changes {
			f.s.say("%s: %s", r.Branch.ShortName(), change)
			f.s.notify(r.Branch.ShortName(), change)
		}
	}
	f.reported = problems
}

// cleaner runs automatic cleanup at most once per CleanupEvery for the
// database, whichever serve runs it: the last run is the time of a stamp
// file beside the database.
type cleaner struct {
	s      *server
	failed string
}

func (c *cleaner) maybe(ctx context.Context) {
	if !c.s.options.Cleanup {
		return
	}
	stamp := c.s.e.serveFile("cleanup.stamp")
	if info, err := os.Stat(stamp); err == nil && time.Since(info.ModTime()) < c.s.options.CleanupEvery {
		return
	}
	// The stamp goes first, so a cleanup that fails is not retried every
	// few seconds; it is tried again the next day, and its problem is
	// reported once until it changes.
	if err := os.WriteFile(stamp, nil, 0o644); err != nil {
		return
	}
	now := time.Now()
	_ = os.Chtimes(stamp, now, now)
	report, err := c.s.e.Cleanup(ctx, c.s.options.CleanupAge)
	if err != nil {
		if problem := fmt.Sprintf("serve: cleanup: %v", err); problem != c.failed {
			c.s.say("%s", problem)
			c.failed = problem
		}
	}
	for _, branch := range report.Branches {
		var removed []string
		for _, step := range branch.Steps {
			if step.Done {
				removed = append(removed, step.What)
			}
		}
		if len(removed) > 0 {
			c.s.say("%s: cleaned up after the merge: removed %s", branch.Branch.ShortName(), strings.Join(removed, ", "))
		}
	}
	if len(report.Indexes) > 0 {
		c.s.say("serve: removed %s unused for %s", plural(len(report.Indexes), "port index generation"), c.s.options.CleanupAge)
	}
}

// OutdatedLook is what serve's daily look at your ports last found, for
// status.
type OutdatedLook struct {
	CheckedAt time.Time `json:"checked_at"`
	Master    string    `json:"master"`
	Outdated  []string  `json:"outdated"`
}

// outdatedScanner looks for new releases of your ports once a day, at the
// configured time, and does what serve.for_outdated says.
type outdatedScanner struct {
	s        *server
	reported string
}

func (o *outdatedScanner) maybe(ctx context.Context) {
	e, settings := o.s.e, o.s.options.Outdated
	now := o.s.options.Now()
	due := time.Date(now.Year(), now.Month(), now.Day(), settings.Hour, settings.Minute, 0, 0, now.Location())
	if now.Before(due) {
		return
	}
	stamp := e.serveFile("outdated.stamp")
	if info, err := os.Stat(stamp); err == nil && !info.ModTime().Before(due) {
		return
	}
	if err := os.WriteFile(stamp, nil, 0o644); err != nil {
		return
	}
	_ = os.Chtimes(stamp, now, now)
	report := func(problem string) {
		if problem != o.reported {
			o.s.say("%s", problem)
			o.reported = problem
		}
	}
	if len(settings.Maintainers) == 0 {
		report(`serve: serve.for_outdated needs to know your ports: set maintainer = "{@you example.org:you}" in ~/.dockhand/config.toml`)
		return
	}
	found, err := e.Outdated(ctx, OutdatedRequest{Maintainers: settings.Maintainers})
	if err != nil {
		report(fmt.Sprintf("serve: looking for new releases of your ports: %v", err))
		return
	}
	var names []string
	for _, port := range found.Ports {
		if port.Outdated {
			names = append(names, port.Port)
		}
	}
	look := OutdatedLook{CheckedAt: now, Master: string(found.Master), Outdated: names}
	if data, err := json.Marshal(look); err == nil {
		_ = os.WriteFile(e.serveFile("outdated.json"), data, 0o644)
	}
	if len(names) == 0 {
		o.s.say("serve: none of your ports has a newer release")
		return
	}
	o.s.say("serve: %s of yours %s newer releases: %s", plural(len(names), "port"), map[bool]string{true: "has", false: "have"}[len(names) == 1], strings.Join(names, ", "))
	if settings.Mode == "list" || settings.Mode == "" {
		return
	}
	plan, err := e.PlanOutdated(ctx, found)
	if err != nil {
		report(fmt.Sprintf("serve: splitting the updates: %v", err))
		return
	}
	prepare := PrepareOptions{Origin: model.OriginServe, Check: settings.Mode == "check", Tests: settings.Tests}
	if prepare.Check {
		if prepare.Environments, err = e.Environments(settings.On); err != nil {
			report(fmt.Sprintf("serve: serve.for_outdated = \"check\": %v", err))
			prepare.Check = false
		}
	}
	for _, done := range e.PrepareOutdated(ctx, plan, prepare) {
		if done.Problem != "" {
			o.s.say("serve: %s: %s", done.Planned.Name, done.Problem)
			continue
		}
		line := fmt.Sprintf("serve: prepared %s: %s → %s", done.Branch.ShortName(), done.Update.Before, done.Update.After)
		if done.Run != nil {
			line += ", " + done.Run.Name() + " queued"
		}
		o.s.say("%s", line)
		o.s.notify(done.Branch.ShortName(), strings.TrimPrefix(line, "serve: "))
	}
}

// LastOutdatedLook is what serve's daily look at your ports last found, if
// it looked.
func (e *Engine) LastOutdatedLook() (OutdatedLook, bool) {
	var look OutdatedLook
	data, err := os.ReadFile(e.serveFile("outdated.json"))
	if err != nil || json.Unmarshal(data, &look) != nil {
		return look, false
	}
	return look, true
}

// passingSubmitter opens pull requests for the branches serve prepared
// whose checks passed, within Design v3 §11's guardrails, at most the
// daily limit, counted from the journal so it holds across restarts.
type passingSubmitter struct {
	s    *server
	last time.Time
	held map[model.BranchID]string
}

func (p *passingSubmitter) maybe(ctx context.Context) {
	if !p.last.IsZero() && time.Since(p.last) < p.s.options.Refresh {
		return
	}
	p.last = time.Now()
	e := p.s.e
	candidates, err := e.ServeCandidates(ctx)
	if err != nil {
		p.s.say("serve: finding passing updates: %v", err)
		return
	}
	for _, candidate := range candidates {
		name := candidate.Branch.ShortName()
		if len(candidate.Held) > 0 {
			if p.held[candidate.Branch.ID] != candidate.Held[0] {
				p.s.say("serve: %s is held for a look: %s", name, candidate.Held[0])
				p.held[candidate.Branch.ID] = candidate.Held[0]
			}
			continue
		}
		opened, err := e.servedToday(ctx, p.s.options.Now())
		if err != nil {
			p.s.say("serve: counting today's pull requests: %v", err)
			return
		}
		if opened >= p.s.options.SubmitLimit {
			p.s.say("serve: %s waits for tomorrow; today's limit of %s is reached (serve.submit_limit)", name, plural(p.s.options.SubmitLimit, "pull request"))
			return
		}
		submitted, err := e.SubmitForServe(ctx, candidate)
		if err != nil {
			p.s.say("serve: submitting %s: %v", name, err)
			continue
		}
		line := fmt.Sprintf("opened #%d for %s, which passed its check", submitted.PullRequest.Ref.Number, name)
		p.s.say("serve: %s", line)
		p.s.notify(name, line)
	}
}

// servedToday counts the pull requests serve opened by itself since the
// start of now's day.
func (e *Engine) servedToday(ctx context.Context, now time.Time) (int, error) {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var count int
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		count, err = r.CountEvents(ServeSubmitKind, midnight)
		return err
	})
	return count, err
}

// Serving is what the leading serve says about itself, for status and
// queue in other terminals.
type Serving struct {
	PID           int  `json:"pid"`
	SubmitPassing bool `json:"submit_passing"`
}

func (e *Engine) announceServing(submitPassing bool) {
	if data, err := json.Marshal(Serving{PID: os.Getpid(), SubmitPassing: submitPassing}); err == nil {
		_ = os.WriteFile(e.serveFile("serving.json"), data, 0o644)
	}
}

// LastServing is what the last serve to lead said about itself; a caller
// compares its PID with the leader's.
func (e *Engine) LastServing() (Serving, bool) {
	var serving Serving
	data, err := os.ReadFile(e.serveFile("serving.json"))
	if err != nil || json.Unmarshal(data, &serving) != nil {
		return serving, false
	}
	return serving, true
}

// serveFile is one of serve's small files beside the database.
func (e *Engine) serveFile(name string) string {
	return filepath.Join(filepath.Dir(e.LogDirectory()), name)
}
