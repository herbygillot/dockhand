package engine

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
)

// ServeOptions are what serve does besides running the queue, as the
// person's configuration and flags set them (Design v3 §11).
type ServeOptions struct {
	// Drain runs what is queued now and returns once the queue is empty.
	Drain bool
	// ShipTo is serve.ship_to: how far serve carries your outdated ports
	// each day, list, or one of ship's stages; at submit, a pull request
	// for each branch serve prepared whose check passed, at most
	// SubmitLimit a day.
	ShipTo      string
	SubmitLimit int
	// Outdated is the daily look for new releases of your ports.
	Outdated ServeOutdated
	// Cleanup turns automatic cleanup on (decision 36), removing what has
	// gone unused for CleanupAge.
	Cleanup    bool
	CleanupAge time.Duration
	// MinFree is the free space below which cleanup runs at once rather
	// than waiting for its day; zero watches none.
	MinFree uint64
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
	// Executable is the file serve runs from, which it watches: replaced,
	// as port upgrade replaces it, serve finishes its checks and stops,
	// and launchd starts the new build. Empty watches none.
	Executable string
	// Build is the build serve runs, which status shows beside its own.
	Build string
	// Identity is who the person's GitHub login acts as now, read afresh
	// each time (github.IdentityWatcher); serve acts on GitHub only as the
	// account it first found. Nil checks nothing.
	Identity func(context.Context) (string, error)
	// IdentityEvery is how often Identity is read; a minute when zero.
	IdentityEvery time.Duration
	// ExecutableGrace is how long the executable may be missing before
	// serve takes it as uninstalled, past an upgrade's moment between its
	// old and new files; a minute when zero.
	ExecutableGrace time.Duration
}

// ServeOutdated is the daily look serve.ship_to carries, and whose ports
// it looks at.
type ServeOutdated struct {
	// Maintainers are your identities; with none, serve says it needs them.
	Maintainers []string
	// Hour and Minute are when it looks each day. A check of each checks
	// with check.tests (Options.Tests), on build.unattended's profile.
	Hour, Minute int
}

// submits is whether serve opens pull requests: ship_to = "submit".
func (o ServeOptions) submits() bool { return o.ShipTo == string(ShipSubmit) }

// Serve leads the repository's queue for a session until ctx ends, or with
// Drain until the queue is empty, and does the rest of serve's work
// between checks (Design v3 §11). With another serve leading it stands by,
// and takes over when that one's session dies; a drain has nothing to do
// then. Runs still going when it stops are left for the next serve.
//
// A drain that found the queue empty reads it once more after letting go
// of the lead, and drains again where a check was queued meanwhile, as
// check -d does where it sees no serve leading (dockhand check -d's drain).
func (e *Engine) Serve(ctx context.Context, session *coord.Session, options ServeOptions) error {
	s := newServer(e, options)
	var again model.RunID
	for {
		lease, err := s.lead(ctx, session)
		if err != nil || lease == nil {
			return err
		}
		err = s.run(ctx, session, *lease)
		session.Release(context.WithoutCancel(ctx), *lease)
		if err != nil || !options.Drain || ctx.Err() != nil {
			return err
		}
		queued, err := e.Runs(ctx, store.RunFilter{States: []model.RunState{model.RunQueued}, Limit: 1})
		// One the drain left queued, it can't run: it's not drained again.
		if err != nil || len(queued) == 0 || queued[0].ID == again {
			return err
		}
		again = queued[0].ID
		s.say("serve: %s was queued as the drain ended; draining again", queued[0].Name())
	}
}

// server is one serve's state while it leads.
type server struct {
	e       *Engine
	options ServeOptions
	mu      sync.Mutex
	// loginEnded is whether serve has notified that the GitHub login
	// can't renew itself, which it does once a process.
	loginEnded atomic.Bool
	// unwritten is the day each of serve's files last failed to be
	// written, said once a day (written).
	unwritten map[string]time.Time
}

func newServer(e *Engine, options ServeOptions) *server {
	if options.Poll <= 0 {
		options.Poll = 2 * time.Second
	}
	if options.Refresh <= 0 {
		options.Refresh = PullRequestRefresh
	}
	if options.CleanupEvery <= 0 {
		options.CleanupEvery = CleanupEvery
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

// written says a failure to write one of serve's files once a day, where
// it was dropped, and status read an older state without a word (the
// architecture review's L3e). Nil says nothing.
func (s *server) written(name string, err error) {
	if err == nil {
		return
	}
	day := dayStart(s.options.Now())
	s.mu.Lock()
	said := s.unwritten[name].Equal(day)
	if !said {
		if s.unwritten == nil {
			s.unwritten = map[string]time.Time{}
		}
		s.unwritten[name] = day
	}
	s.mu.Unlock()
	if !said {
		s.say("serve: couldn't write %s, so status may read an older state: %v", s.e.serveFile(name), err)
	}
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
			s.say("serve: standing by; another instance (pid %d) is running the queue", held.Holder.PID)
			announced = true
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(s.options.Poll):
		}
	}
}

// capacityWords says how many checks a provider takes at once. Capacity
// counts checks, and a check may build several of its environments at
// once, as Tart builds two releases in the Mac's two VMs, which "1 at a
// time" hid (the sshuttle run).
func capacityWords(name string, provider buildenv.Provider, checks int) string {
	words := fmt.Sprintf("%s (%s at a time", name, prose.Plural(checks, "check"))
	if parallel, ok := provider.(buildenv.ParallelProvider); ok && parallel.Parallel() > 1 {
		words += fmt.Sprintf(", each building up to %d of its environments at once", parallel.Parallel())
	}
	return words + ")"
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
		capacity[name] = e.capacity(name)
		described = append(described, capacityWords(name, e.Providers[name], capacity[name]))
	}
	if len(described) == 0 {
		described = []string{"none set up; checks will need attention"}
	}
	publishing := "opens no pull requests; it only checks"
	if options.submits() {
		publishing = fmt.Sprintf("opens PRs for passing updates it prepared, at most %d a day", options.SubmitLimit)
	}
	// The build serve started from: after port upgrade replaced it, a
	// serve under launchd went on running the old one, and status said
	// nothing of it (the rc3 full stage, A7). An upgrade is noticed between
	// checks, never during one (Design v3 §11): serve starts no more, and
	// stops once those running end, so launchd starts the new build. It's
	// noted before serve says it leads, so nothing replaced after that
	// line passes for the build it started from.
	started := executableFile(options.Executable)
	upgraded := false
	s.say("serve: leading (pid %d) · builds on %s · %s", os.Getpid(), strings.Join(described, ", "), publishing)
	login := &loginGate{s: s}
	login.look(ctx)
	s.written("serving.json", e.announceServing(login.serving()))
	followed := &follower{s: s, reported: map[string]bool{}}
	cleaned := &cleaner{s: s, session: session}
	scanned := &outdatedScanner{s: s}
	if !options.Drain {
		scanned.announce(ctx)
	}
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
	// ended are the runs whose end serve said, each said once: field
	// testing saw "check-100 lego-7ny0: passed" twice, from a cause not
	// found (the ninth report).
	ended := map[model.RunID]bool{}
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
			s.say("%s: unfinished; the next serve instance will resume it", f.run.Name())
			return
		}
		if ended[f.run.ID] {
			return
		}
		ended[f.run.ID] = true
		line := fmt.Sprintf("%s %s: %s", f.run.Name(), f.branch.ShortName(), f.run.State)
		if f.run.Detail != "" && f.run.State != model.RunPassed {
			line += ": " + f.run.Detail
		}
		s.say("%s", line)
		s.notify(f.branch.ShortName(), line)
		// A check that passed may be what the submitter waits for.
		submitter.last = time.Time{}
	}
	var missing time.Time
	stopped := ""
	for running.Err() == nil {
		if !upgraded && started != nil {
			switch gone, swapped := replaced(options.Executable, started); {
			case swapped:
				upgraded, stopped = true, "serve: stopped for the upgrade"
				s.say("serve: upgrade detected; waiting for active checks to finish before exiting so launchd can start the new version")
			case !gone:
				missing = time.Time{}
			case missing.IsZero():
				missing = time.Now()
			case time.Since(missing) >= cmp.Or(options.ExecutableGrace, time.Minute):
				// Gone past an upgrade's moment between its old and new
				// files: uninstalled. serve went on running the deleted
				// program (the rc6 full stage, A8).
				upgraded, stopped = true, "serve: stopped, as dockhand was uninstalled"
				s.say("serve: dockhand was uninstalled; stopping once its checks end. serve --uninstall removes the agent")
			}
		}
		if upgraded {
			if len(inFlight) == 0 {
				s.say("%s", stopped)
				return nil
			}
			select {
			case <-running.Done():
			case f := <-done:
				settle(f)
			}
			continue
		}
		if !options.Drain {
			login.look(running)
			if login.reads() {
				followed.maybe(running)
				scanned.maybe(running)
			}
			if options.submits() && login.writes() {
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
			// A check on GitHub pushes as the login: none starts while it
			// isn't the account serve started as.
			if slices.Contains(needs, buildenv.GitHub) && !login.writes() {
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
				// A drain follows no pull request after it ends, which a
				// person reading "the queue is empty" took it to (SQL-O8's
				// item, the person's decision 6).
				if open := e.followedPullRequests(running); len(open) > 0 {
					s.say("serve: %s remain open: %s. Monitoring stops when --drain exits; run dockhand serve to keep monitoring them", prose.Plural(len(open), "pull request"), prose.And(open))
				}
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

// loginGate keeps serve acting on GitHub only as the account it first
// found: a serve went on with a token after a logout, and once that
// expired could have gone on as the GitHub CLI's account, another one (the
// rc6 full stage, D-C2). It looks at most once a minute. With no login,
// serve reads pull requests without one and writes nothing; with another
// account's, it does nothing on GitHub; each is said once, and the account
// it started as coming back is said too.
type loginGate struct {
	s      *server
	pinned string
	// state is "" while serve acts, "none" with no login, "rejected" with
	// one GitHub no longer honours, and "other" with another account's,
	// other.
	state, other string
	last         time.Time
	// problem is why the login couldn't be read, the last look; "" when
	// it could (unread).
	problem string
}

func (g *loginGate) look(ctx context.Context) {
	identity := g.s.options.Identity
	if identity == nil || !g.last.IsZero() && time.Since(g.last) < cmp.Or(g.s.options.IdentityEvery, time.Minute) {
		return
	}
	g.last = time.Now()
	account, err := identity(ctx)
	if errors.Is(err, github.ErrNoCredentials) || errors.Is(err, github.ErrAuthentication) {
		g.unread(nil)
	} else {
		g.unread(err)
	}
	switch {
	case errors.Is(err, github.ErrNoCredentials):
		g.set("none", "")
	case errors.Is(err, github.ErrAuthentication):
		// GitHub no longer honours the login, as once it's revoked: the
		// credential is there and unchanged, and serve would have tried it
		// each look, saying nothing in status (the rc6 full stage, D-C3).
		g.set("rejected", "")
	case err != nil:
		// Who it is couldn't be read now, as GitHub out of reach; what
		// serve does stays as it was, and unread said why.
	case g.pinned == "":
		g.pinned = account
		g.set("", "")
	case account != g.pinned:
		g.set("other", account)
	default:
		g.set("", "")
	}
}

func (g *loginGate) set(state, other string) {
	if state == g.state && other == g.other {
		return
	}
	was := g.state
	g.state, g.other = state, other
	// The client held, and its token, go: the next use reads the login as
	// it is now.
	g.s.e.dropForge()
	// What status and queue read is written before serve says it, so one
	// that follows serve's line reads it: D-C3 read queue right after the
	// rejection's line and found serve acting (the rc9 full stage).
	g.s.written("serving.json", g.s.e.announceServing(g.serving()))
	switch state {
	case "none":
		g.s.say("serve: no GitHub credentials are available. Pull requests will be read anonymously; submissions, pushes, and GitHub checks are paused. Run dockhand setup github to sign in")
	case "other":
		g.s.say("serve: GitHub is now signed in as %s; serve started as %s. GitHub writes are paused. Sign in as %s again, or restart serve to use the current account", other, g.pinned, g.pinned)
	case "rejected":
		g.s.say("serve: GitHub rejected the credentials. GitHub writes are paused; run dockhand setup github to sign in again")
	default:
		if was != "" {
			g.s.say("serve: logged in again as %s; acting on GitHub again", g.pinned)
		}
	}
}

// serving is what serve says of itself, with its login as it stands.
func (g *loginGate) serving() Serving {
	return Serving{SubmitPassing: g.s.options.submits(), Build: g.s.options.Build, NotActing: g.words(), LoginProblem: g.problem}
}

// unread says once why who the login is couldn't be read, where it went
// unsaid and serve kept its state, as "rejected" after a new login it
// couldn't read (the rc8 full stage's D-C3); nil clears it, said by the
// login's state. It's said again when the reason changes.
func (g *loginGate) unread(err error) {
	problem := ""
	if err != nil {
		problem = err.Error()
	}
	if problem == g.problem {
		return
	}
	g.problem = problem
	g.s.written("serving.json", g.s.e.announceServing(g.serving()))
	if problem != "" {
		g.s.say("serve: could not verify the GitHub account (%s). Continuing with the current authentication state; retrying each minute", problem)
	}
}

// reads is whether serve reads GitHub; writes, whether it acts there.
func (g *loginGate) reads() bool  { return g.state != "other" && g.state != "rejected" }
func (g *loginGate) writes() bool { return g.state == "" }

// words are what status says of a serve not acting on GitHub; empty
// while it acts.
func (g *loginGate) words() string {
	switch g.state {
	case "none":
		return "no GitHub login"
	case "other":
		return fmt.Sprintf("logged in as %s, started as %s", g.other, g.pinned)
	case "rejected":
		return "GitHub rejected the login; dockhand setup github"
	}
	return ""
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
	if err == nil {
		err = refreshed.Refused
	}
	problems := map[string]bool{}
	report := func(problem string) {
		problems[problem] = true
		if !f.reported[problem] {
			f.s.say("%s", problem)
		}
	}
	switch {
	// A login that can't renew itself is said once, and notified once a
	// process, rather than as each pull request it couldn't read (the
	// auth flow review's plan, step 4). It refuses the whole read, which
	// reported it as any other refusal, unnotified, since the read stopped
	// at the first refusal (SQL-O8's refresh).
	case errors.Is(err, github.ErrLoginEnded):
		report("serve: GitHub credentials could not be renewed, so pull request refresh is paused. Run dockhand setup github to sign in again; serve will use the new credentials automatically")
		if f.s.loginEnded.CompareAndSwap(false, true) {
			f.s.notify("GitHub login", "GitHub login expired or cannot be renewed. Sign in again: dockhand setup github")
		}
	case err != nil:
		report(fmt.Sprintf("serve: could not read pull requests: %v", err))
	}
	if words := refreshed.UnreadWords(); words != "" {
		report("serve: " + words)
	}
	for _, r := range refreshed.Branches {
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
	s       *server
	session *coord.Session
	failed  string
}

func (c *cleaner) maybe(ctx context.Context) {
	if !c.s.options.Cleanup {
		return
	}
	report, ran, err := c.s.e.CleanupIfDue(ctx, c.session, c.s.options.CleanupEvery, c.s.options.MinFree, c.s.options.CleanupAge, func(why CleanupReason) {
		if why.LowSpace {
			c.s.say("cleaning up now: %s", why.Words)
		}
	})
	if !ran {
		return
	}
	// A cleanup that fails is tried again the next day, and its problem
	// is reported once until it changes.
	if err != nil {
		if problem := fmt.Sprintf("serve: cleanup: %v", err); problem != c.failed {
			c.s.say("%s", problem)
			c.failed = problem
		}
	}
	for _, line := range report.Words(c.s.options.CleanupAge) {
		c.s.say("%s", line)
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
	for _, leftover := range report.Leftovers {
		if leftover.Done {
			c.s.say("%s: removed %s, left when its process ended", leftover.Run.Name(), leftover.What)
		}
	}
	if len(report.Indexes) > 0 {
		c.s.say("serve: removed %s unused for %s", prose.Plural(len(report.Indexes), "port index generation"), c.s.options.CleanupAge)
	}
}

// OutdatedLook is what serve's daily look at your ports last found, for
// status.
type OutdatedLook struct {
	CheckedAt time.Time `json:"checked_at"`
	Outdated  []string  `json:"outdated"`
	// Uncertain are the ports that may have a newer release, which serve
	// neither calls current nor prepares: a person looks. SetAside are the
	// tags each was uncertain for, which the next look reads to say a port
	// again only where they changed.
	Uncertain []string            `json:"uncertain,omitempty"`
	SetAside  map[string][]string `json:"set_aside,omitempty"`
}

// uncertainWords are the day's line of the ports that may have newer
// releases: each named where what was set aside is new since the last
// look, and the rest, said already, counted. A port with a misspelled old
// tag, as dolt's v040.15, is uncertain every day it's otherwise current,
// and named once, not every morning. Empty where there is none to say.
func uncertainWords(last OutdatedLook, look OutdatedLook) string {
	var fresh []string
	for _, port := range look.Uncertain {
		if !slices.Equal(last.SetAside[port], look.SetAside[port]) {
			fresh = append(fresh, port)
		}
	}
	already := len(look.Uncertain) - len(fresh)
	switch {
	case len(fresh) == 0 && already == 0:
		return ""
	case len(fresh) == 0:
		return fmt.Sprintf("serve: %s of yours may still have newer releases, as before (dockhand status lists them)", prose.Plural(already, "port"))
	}
	line := fmt.Sprintf("serve: %s may have newer releases that need review: %s. Run dockhand outdated %s for details", prose.Plural(len(fresh), "port"), strings.Join(fresh, ", "), strings.Join(fresh, " "))
	if already > 0 {
		line += fmt.Sprintf("; %d more as before", already)
	}
	return line
}

// lookNotice is the day's look as a notification, where the ports it
// found with newer releases aren't the last look's: serve notified of a
// pull request, a branch it prepared, and a submission, and the look's
// count went to serve.log and status alone, so serve.ship_to =
// "list", the default, said nothing a person saw (the rc10 full stage's
// B7). Empty where nothing changed, or none has a newer release. It names
// outdated --mine, which lists them.
func lookNotice(last, look OutdatedLook) string {
	if len(look.Outdated) == 0 || slices.Equal(slices.Sorted(slices.Values(last.Outdated)), slices.Sorted(slices.Values(look.Outdated))) {
		return ""
	}
	return fmt.Sprintf("%s newer releases (dockhand outdated --mine)", prose.Plural(len(look.Outdated), "port")+map[bool]string{true: " has", false: " have"}[len(look.Outdated) == 1])
}

// outdatedScanner looks for new releases of your ports once a day, at the
// configured time, and carries them as far as serve.ship_to says.
type outdatedScanner struct {
	s        *server
	reported string
	// retryAt is when the day's look goes over the ports a rate limit
	// kept it from checking, retry, once, just after the limit lifts; the
	// day isn't done until then (the rc6 full stage, D-N4).
	retryAt time.Time
	retry   []string
}

// hint is serve's line for a person whose config names no maintainer.
func (o *outdatedScanner) hint(ctx context.Context) string {
	return "serve: serve.ship_to needs to know your ports: " + o.s.e.MaintainerHint(ctx)
}

// announce says at once what the day's look would say of a config that
// names no maintainer, which a serve started after the day's look had run
// otherwise never said (the dogfood run with 251a1264); the look says it
// again only where it changed.
func (o *outdatedScanner) announce(ctx context.Context) {
	if len(o.s.options.Outdated.Maintainers) == 0 {
		line := o.hint(ctx)
		o.s.say("%s", line)
		o.reported = line
	}
}

func (o *outdatedScanner) maybe(ctx context.Context) {
	e, settings := o.s.e, o.s.options.Outdated
	now := o.s.options.Now()
	due := time.Date(now.Year(), now.Month(), now.Day(), settings.Hour, settings.Minute, 0, 0, now.Location())
	if now.Before(due) {
		return
	}
	if info, err := os.Stat(e.serveFile("outdated.stamp")); err == nil && !info.ModTime().Before(due) {
		return
	}
	retrying := len(o.retry) > 0
	if retrying && now.Before(o.retryAt) {
		return
	}
	// The day's look is stamped once it has run, so a serve stopped part
	// way through looks again; one that failed is stamped too, and tried
	// the next day, its problem said once. One a rate limit cut short
	// isn't, until its retry has run.
	stamp := true
	defer func() {
		if ctx.Err() == nil && stamp {
			o.s.written("outdated.stamp", e.stampServeFile("outdated.stamp", now))
		}
	}()
	report := func(problem string) {
		if problem != o.reported {
			o.s.say("%s", problem)
			o.reported = problem
		}
	}
	if len(settings.Maintainers) == 0 {
		// The person's own line, as master's ports write their GitHub
		// login, where it can be found (the flyctl run).
		report(o.hint(ctx))
		return
	}
	request := OutdatedRequest{Maintainers: settings.Maintainers}
	if retrying {
		request = OutdatedRequest{Ports: o.retry}
		o.retry, o.retryAt = nil, time.Time{}
	}
	found, err := e.Outdated(ctx, request)
	if err != nil {
		report(fmt.Sprintf("serve: looking for new releases of your ports: %v", err))
		return
	}
	if limited, lifts := found.RateLimited(); len(limited) > 0 {
		if !retrying {
			o.retry, o.retryAt, stamp = limited, lifts.Add(time.Minute), false
			o.s.say("serve: GitHub’s rate limit prevented checking %s for new releases. Retrying at %s", prose.Plural(len(limited), "port"), o.retryAt.Local().Format("15:04 MST"))
		} else {
			o.s.say("serve: GitHub’s rate limit still prevents checking %s for new releases. No further retries today", prose.Plural(len(limited), "port"))
		}
	}
	var names, uncertain []string
	setAside := map[string][]string{}
	for _, port := range found.Ports {
		switch port.Class() {
		case OutdatedNewer:
			names = append(names, port.Port)
		case OutdatedUncertain:
			uncertain = append(uncertain, port.Port)
			for _, aside := range port.Uncertain {
				setAside[port.Port] = append(setAside[port.Port], aside.Tag)
			}
		case OutdatedAhead:
			// Ahead of every version upstream lists: a look's too, as
			// status says, and never prepared.
			uncertain = append(uncertain, port.Port)
			setAside[port.Port] = []string{port.Ahead}
		}
	}
	last, _ := e.LastOutdatedLook()
	look := OutdatedLook{CheckedAt: now, Outdated: names, Uncertain: uncertain, SetAside: setAside}
	// A retry's look adds to the day's, which it went over only part of.
	if retrying {
		for _, name := range last.Outdated {
			if !slices.Contains(look.Outdated, name) {
				look.Outdated = append(look.Outdated, name)
			}
		}
	}
	if data, err := json.Marshal(look); err == nil {
		o.s.written("outdated.json", e.writeServeFile("outdated.json", data))
	}
	if line := lookNotice(last, look); line != "" {
		o.s.notify("Your ports", line)
	}
	// What couldn't be checked is said first, and the rest's result as the
	// rest's: "none of your ports" said of ports it never saw (D-N4).
	unchecked := found.UncheckedWords()
	if unchecked != "" {
		o.s.say("serve: %s", unchecked)
	}
	switch {
	case len(names) == 0 && unchecked != "":
		o.s.say("serve: none of the rest has a newer release")
	case len(names) == 0:
		o.s.say("serve: none of your ports has a newer release")
	default:
		o.s.say("serve: %s of yours %s newer releases: %s", prose.Plural(len(names), "port"), map[bool]string{true: "has", false: "have"}[len(names) == 1], strings.Join(names, ", "))
	}
	// A port whose newest release is uncertain is neither current nor an
	// update serve prepares: it is listed for a person, who names the
	// version to take, once for what was set aside.
	if line := uncertainWords(last, look); line != "" {
		o.s.say("%s", line)
	}
	to := ShipStage(o.s.options.ShipTo)
	if len(names) == 0 || to != ShipCommit && to != ShipCheck && to != ShipSubmit {
		return
	}
	plan, err := e.PlanOutdated(ctx, found)
	if err != nil {
		report(fmt.Sprintf("serve: splitting the updates: %v", err))
		return
	}
	// What serve prepares nobody looks over.
	prepare := PrepareOptions{Origin: model.OriginServe, To: to, Answered: model.AnsweredNobody}
	if to == ShipSubmit {
		// Checked here; serve's submitter submits what passes.
		prepare.To = ShipCheck
	}
	if prepare.To == ShipCheck {
		if prepare.Where, err = e.Where(ctx, WhereRequest{Unattended: true}); err != nil {
			report(fmt.Sprintf("serve: serve.ship_to = %q: %v", to, err))
			prepare.To = ShipCommit
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
	// yours are the passing branches a person started, as last said.
	yours string
}

func (p *passingSubmitter) maybe(ctx context.Context) {
	if !p.last.IsZero() && time.Since(p.last) < p.s.options.Refresh {
		return
	}
	p.last = time.Now()
	e := p.s.e
	candidates, yours, err := e.serveCandidates(ctx)
	if err != nil {
		p.s.say("serve: finding passing updates: %v", err)
		return
	}
	// The passing branches a person started are theirs to submit, and
	// said once for each set of them.
	var names []string
	for _, branch := range yours {
		names = append(names, branch.ShortName())
	}
	if said := strings.Join(names, " "); said != p.yours {
		p.yours = said
		if len(names) > 0 {
			whose := "they're"
			if len(names) == 1 {
				whose = "it's"
			}
			p.s.say("serve: %s you started passed (%s); serve submits only what it prepared, so %s yours: dockhand ship --passing", prose.Plural(len(names), "branch"), strings.Join(names, ", "), whose)
		}
	}
	for _, candidate := range candidates {
		name := candidate.Branch.ShortName()
		if len(candidate.Held) > 0 {
			// Every concern is said, where only the first was (C2), once a
			// branch while they stay the same.
			var keys []string
			for _, concern := range candidate.Held {
				keys = append(keys, concern.Key())
			}
			if key := strings.Join(keys, "\x01"); p.held[candidate.Branch.ID] != key {
				p.s.say("serve: %s needs review before submission: %s", name, strings.Join(HoldLines(candidate.Held), "; "))
				p.held[candidate.Branch.ID] = key
			}
			continue
		}
		opened, err := e.servedToday(ctx, p.s.options.Now())
		if err != nil {
			p.s.say("serve: counting today's pull requests: %v", err)
			return
		}
		if opened >= p.s.options.SubmitLimit {
			p.s.say("serve: %s waits for tomorrow; today's limit of %s is reached (serve.submit_limit)", name, prose.Plural(p.s.options.SubmitLimit, "pull request"))
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
// start of now's day. Cleanup keeps the day's events for it (Cleanup).
func (e *Engine) servedToday(ctx context.Context, now time.Time) (int, error) {
	var count int
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		count, err = r.CountEvents(model.EventServeSubmit, dayStart(now))
		return err
	})
	return count, err
}

// dayStart is midnight at the start of t's day, where t is: the day
// serve.submit_limit counts.
func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// executableFile is the file serve runs from, as it was when serve
// started; nil where there's none to watch.
func executableFile(path string) os.FileInfo {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return info
}

// replaced says whether the file at path is gone, or is no longer the one
// serve started from: another file, as an upgrade installs, or the same
// one rewritten.
func replaced(path string, started os.FileInfo) (gone, swapped bool) {
	now, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, false
	}
	if err != nil {
		return false, false
	}
	return false, !os.SameFile(started, now) || !now.ModTime().Equal(started.ModTime()) || now.Size() != started.Size()
}

// Serving is what the leading serve says about itself, for status and
// queue in other terminals.
type Serving struct {
	PID           int    `json:"pid"`
	SubmitPassing bool   `json:"submit_passing"`
	Build         string `json:"build,omitempty"`
	// NotActing is why serve isn't acting on GitHub, as its login stands;
	// empty while it acts.
	NotActing string `json:"not_acting,omitempty"`
	// LoginProblem is why serve couldn't read who its login is, the last
	// time it looked; empty when it could.
	LoginProblem string `json:"login_problem,omitempty"`
}

// ServeState is serve's state for the repository, as status
// show it: whether a serve leads it, as which process, and whether that
// serve opens pull requests for the updates it prepared that pass; and the
// checks queued or running, with how many of those have stopped.
type ServeState struct {
	Running           bool
	PID               int
	OpensPullRequests bool
	// Build is the build the leading serve runs, where it said; NotActing,
	// why it isn't acting on GitHub, where it isn't; LoginProblem, why it
	// couldn't read its login, where it couldn't.
	Build        string
	NotActing    string
	LoginProblem string
	Queue        int
	Stopped      int
}

// Upgraded is the build a running serve runs where it isn't this
// process's, as one port upgrade left running (the rc3 full stage, A7);
// false where it's the same, or unknown.
func (s ServeState) Upgraded() (string, bool) {
	return s.Build, s.Running && s.Build != "" && s.Build != buildinfo.Current().String()
}

// ServeState reads serve's state. Judging who is alive takes a session.
func (e *Engine) ServeState(ctx context.Context, session *coord.Session) (ServeState, error) {
	queued, err := e.Runs(ctx, store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}})
	if err != nil {
		return ServeState{}, err
	}
	state := ServeState{Queue: len(queued)}
	for _, run := range queued {
		stopped, err := Stopped(ctx, session, run)
		if err != nil {
			return ServeState{}, err
		}
		if stopped {
			state.Stopped++
		}
	}
	leader, err := session.Holder(ctx, coord.LeaderResource)
	if err != nil {
		return ServeState{}, err
	}
	if leader != nil {
		state.Running, state.PID = true, leader.PID
		// What an earlier serve said of itself isn't the leader's to say.
		if serving, ok := e.lastServing(); ok && serving.PID == leader.PID {
			state.OpensPullRequests, state.Build, state.NotActing, state.LoginProblem = serving.SubmitPassing, serving.Build, serving.NotActing, serving.LoginProblem
		}
	}
	return state, nil
}

// announceServing writes what this serve says of itself, for status, as
// this process.
func (e *Engine) announceServing(serving Serving) error {
	serving.PID = os.Getpid()
	data, err := json.Marshal(serving)
	if err != nil {
		return err
	}
	return e.writeServeFile("serving.json", data)
}

// lastServing is what the last serve to lead said about itself, which is
// the leader's to say only while their PIDs match.
func (e *Engine) lastServing() (Serving, bool) {
	var serving Serving
	data, err := os.ReadFile(e.serveFile("serving.json"))
	if err != nil || json.Unmarshal(data, &serving) != nil {
		return serving, false
	}
	return serving, true
}

// serveFile is one of serve's small files, kept beside the database for
// the repository, as serve's lease and the journal are: one database may
// serve several checkouts, and each keeps its own day.
func (e *Engine) serveFile(name string) string {
	return filepath.Join(filepath.Dir(e.LogDirectory()), "serve", string(e.Repository), name)
}

// writeServeFile writes one of serve's small files.
func (e *Engine) writeServeFile(name string, data []byte) error {
	path := e.serveFile(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// stampServeFile marks one of serve's stamps with when.
func (e *Engine) stampServeFile(name string, when time.Time) error {
	if err := e.writeServeFile(name, nil); err != nil {
		return err
	}
	return os.Chtimes(e.serveFile(name), when, when)
}
