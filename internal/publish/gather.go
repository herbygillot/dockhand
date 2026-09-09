package publish

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// Gather is the FACTS stage the grid omitted for Promote (open question
// "does promote still observe before it publishes?" — it observes the
// FORGE, and never observes, judges or settles runs). It reads the
// change and its attempts from the STATE REF, the branch, the tip and
// the commits beyond the mint from git, the forge's standing live under
// ForgeRefresh (Fresh true) or from the record under ForgeAsCached,
// Direction over upstream main's tip, change.Behind for drift, Closes
// from record.Change.ClosesTicket, and Simplicity Unjudged on the human
// road — the human road never reconstructs. It is a function and not a
// method on Env because Env is the EFFECT surface and gathering performs
// none; it takes Env only for the handles.
//
// An Active attempt on the tip is reported on Facts and becomes
// Permit.Running — an advisory naming `dockhand cancel`. A finished but
// unsettled attempt is a remedy line by residency ("`dockhand status`
// settles it" / "the scheduler will settle it"); a promote that settled
// it would be a second judge.
//
// WHAT IT RETURNS AS AN ERROR AND WHAT IT RETURNS AS A FACT is the whole
// of rule 7 applied at this stage. A read that FAILED — git would not
// answer, the store could not be opened, the change is not in the store
// — is an incident and comes back as an error, because there are no
// facts to decide over. A question that was asked and not answered — the
// forge was unreachable, the evaluation could not be made, the drift
// could not be measured — comes back ON the facts with its own Err, and
// Authorize decides what each road does about it: the machine refuses,
// the person is advised. A gatherer that collapsed the two would make a
// rate-limited forge look like a broken repository.
func Gather(ctx context.Context, e Env, ref change.Ref, policy ForgePolicy, asks Asks, invoker record.Driver, now time.Time) (Facts, error) {
	if policy == ForgeUnset {
		return Facts{}, ErrForgePolicyUnset
	}
	if e.Repo == nil || e.State == nil {
		return Facts{}, ErrNoEnv
	}
	st, err := e.State.Read(ctx)
	if err != nil {
		return Facts{}, err
	}
	c, ok := st.Changes[string(ref.ID())]
	if !ok {
		return Facts{}, ErrNoChange
	}
	if c.Branch == "" {
		return Facts{}, ErrNoBranch
	}
	f := Facts{
		Change:    c,
		Attempts:  attemptsFor(st, c),
		Branch:    c.Branch,
		Tip:       ref.Tip(),
		Invoker:   invoker,
		Asks:      asks,
		BodyLimit: gh.MaxPRBody,
		AsOf:      now,
		Spent:     Spent(st, now),
	}
	// The primary branch at its LOCAL position, resolved once and used
	// TWICE: the base change.Behind measures drift against, and the FROM
	// side of the version movement. One read, because a gather that
	// resolved it per question could answer two of them about different
	// commits (rule 2: two moments for one value is the shape this design
	// forbids).
	primary, err := e.Repo.PrimaryBranch(ctx)
	if err != nil {
		return Facts{}, err
	}
	// IT USED TO BE USED A THIRD TIME, as the base the branch's own
	// commits are measured against, and that was wrong the moment those
	// two commits could differ.
	//
	// A branch's own commits are what it adds ON TOP OF WHAT IT WAS BASED
	// ON, which is record.Change.Base.Sha and nothing else. While every
	// change was cut from the checkout's local primary the two were the
	// same commit and the error was invisible; D29 bases a change on
	// upstream's freshly fetched tip, so a checkout ten commits behind
	// made Own eleven commits — the mint plus every commit the fetch had
	// brought in.
	//
	// MEASURED, ON TWO LIVE PULL REQUESTS. Both came out titled
	// "debianutils: Update to 5.24" — an upstream commit neither change
	// touched — because title() takes Own's last entry, which rev-list
	// order makes the OLDEST once the range is wrong. The same count
	// drives body.go's `single`, so both commit-guideline boxes went
	// unchecked on changes that carry exactly one commit each. One wrong
	// range, three wrong statements to a reviewer.
	//
	// The fallback is primary, for a change whose record carries no base
	// — an adopted branch dockhand did not mint — where "what it was based
	// on" genuinely is not recorded.
	own := c.Base.Sha
	if own == "" {
		own = primary
	}
	if f.Own, err = e.Repo.OwnCommits(ctx, f.Tip, own); err != nil {
		return Facts{}, err
	}
	if f.Title, err = title(ctx, e, asks, f); err != nil {
		return Facts{}, err
	}
	if f.Drift, err = change.Behind(ctx, e.Repo, c, primary); err != nil {
		return Facts{}, err
	}
	f.Direction = direction(ctx, e, c, primary, f.Tip, now)
	switch policy {
	case ForgeRefresh:
		f.Forge = askForge(ctx, e, f)
	case ForgeAsCached:
		f.Forge = cachedForge(st, c)
	case ForgeUnset:
		// Unreachable: refused at the top, before anything was read. Named
		// so a policy added later is a compile-time visit here.
		return Facts{}, ErrForgePolicyUnset
	}
	f.Body = body(f, e.Version)
	return f, nil
}

// attemptsFor is the evidence for a content, in a stable order, so two
// gathers over one store answer with the same list and a golden can pin
// what a body says about it.
//
// IT ASKS CONTENT AND NOT THE CHANGE ID, which is what this package's
// own design says two doc comments above verdicts: "an attempt earned
// over the same content IS evidence for these bytes". verdicts and
// evidenceAt already filter on a.Content; this was the candidate set
// they drew from, and it disagreed with them.
//
// WHAT THE DISAGREEMENT COST: run.Adoptable matches on content, spec and
// platform — never on a change — so a change that ADOPTS a passing
// attempt owns none of its own. Every consumer here then found zero
// attempts and concluded nothing had been run. Measured in the field on
// macports-ports#34586: a delve bump built clean in a VM, `--replace`
// minted a second change over the identical tree, adoption reused the
// pass, and the pull request told reviewers "no verification environment
// on the submitting machine". The build was on the same machine, minutes
// earlier, and had passed.
//
// A ContentID is the git tree OID of the WHOLE resulting tree —
// GraftTree(base, files) when minted, sha^{tree} when adopted — so two
// changes sharing one are byte-identical trees and a moved base yields a
// different id. There is no way for this to gather evidence about
// different bytes.
//
// AND THE PORTS MUST MEET, which content alone does not settle for a
// SNAPSHOT. `verify <port>` on an unmodified checkout writes nothing, so
// its content is just the tree — identical for every snapshot of that
// checkout whatever port it names. Measured in the field the moment this
// was first built: four jq snapshots and two oniguruma6 snapshots all
// carried content 709b8d48, and jq's verdict became oniguruma6's. A
// minted change never collides that way, because GraftTree folds its own
// edits in.
//
// So an attempt is evidence when it built these bytes AND it is about a
// port this change names — record.Attempt.Members is the roster's own
// answer to "which ports is this attempt about", present from the moment
// it is enqueued. A change's OWN attempts are always evidence whatever
// the rosters say: that is today's rule, and this may only ever add to
// it.
//
// AUTHORITY IS NOT SHARED THE SAME WAY, and that line is the whole
// safety of this. Which attempts a change may STOP, withdraw, or hand a
// lease back for stays keyed to the change id, everywhere it already is:
// evidence is about the bytes, and acting on another change's run would
// be a far worse defect than the one this fixes.
func attemptsFor(s statestore.State, c record.Change) []record.Attempt {
	var out []record.Attempt
	if c.Content == "" {
		return nil // a change with no content owns no bytes to be proven
	}
	for _, key := range slices.Sorted(maps.Keys(s.Attempts)) {
		if a := s.Attempts[key]; isEvidenceFor(a, c) {
			out = append(out, a)
		}
	}
	return out
}

// isEvidenceFor is the one rule: an attempt is this change's evidence when
// it is the change's own, or when it built the same bytes and ran a port
// the change names. See attemptsFor for why both halves are needed.
func isEvidenceFor(a record.Attempt, c record.Change) bool {
	if a.Change == c.ID {
		return true
	}
	if a.Content != c.Content {
		return false
	}
	for _, m := range a.Members() {
		for _, s := range c.Subjects {
			if s.Port == m {
				return true
			}
		}
	}
	return false
}

// running names the verifications still in flight on the tip being
// published: an Active attempt of this change whose Sha is the tip. It
// is an ADVISORY and never a refusal — see Permit.Running — and it names
// the platform rather than the attempt id, because what a person does
// about it is `dockhand cancel` on a branch and the platform is what
// tells them which build they would be stopping.
func running(f Facts) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range f.Attempts {
		// The content and not the sha, for attemptsFor's reason: an
		// adopted attempt is building THESE BYTES under another change's
		// commit, and a person told "nothing is running" while a guest
		// works on their content has been told the wrong thing.
		if a.Content != f.Change.Content || !a.Active() || seen[a.Platform] {
			continue
		}
		seen[a.Platform] = true
		out = append(out, a.Platform)
	}
	return out
}

// title is what the pull request will be called.
//
// The ask wins where a person named one. Otherwise it is the subject of
// the branch's OLDEST own commit — the one dockhand minted, whose
// message is already in the project's `<port>: <description>` form
// (change.Message writes it from the plan's Summary). Later commits are
// fixups whose subjects would make bad titles, and rev-list order puts
// the mint last.
func title(ctx context.Context, e Env, asks Asks, f Facts) (string, error) {
	if asks.Title != "" {
		return asks.Title, nil
	}
	subject := f.Tip
	if len(f.Own) > 0 {
		subject = f.Own[len(f.Own)-1]
	}
	return e.Repo.Subject(ctx, subject)
}

// direction is the version movement this publication is about, read at
// publication time and over the tree the change would land in.
//
// THE FROM SIDE IS UPSTREAM MAIN'S TIP and not record.Change.Base.Sha,
// which is the whole of why this is observed here rather than carried
// from the mint: the install a downgrade strands is whatever main ships
// at merge, and a change cut a week ago against a base main has moved
// past compares the wrong pair.
//
// Every way it can fail to answer rides back on Err with the Movement
// left uncompared, because "I could not compare" is an ANSWER the
// machine road acts on and never an incident: a person may publish a
// change to anything they could type, including a downgrade, on "the
// operator typed it", and a gather that returned an error here would
// stop the road that does not care.
func direction(ctx context.Context, e Env, c record.Change, primary, tip string, now time.Time) Direction {
	d := Direction{AsOf: now}
	portdir := headline(c).Portdir
	switch {
	case portdir == "":
		d.Err = fmt.Errorf("%w: the change names no headline portdir", ErrDirectionUnknown)
		return d
	case e.Eval == nil:
		d.Err = fmt.Errorf("%w: no evaluator was wired for this road", ErrDirectionUnknown)
		return d
	}
	from, err := e.Eval.IdentityAt(ctx, primary, portdir)
	if err != nil {
		d.Err = fmt.Errorf("%w: evaluating %s at %s: %w", ErrDirectionUnknown, portdir, primary, err)
		return d
	}
	to, err := e.Eval.IdentityAt(ctx, tip, portdir)
	if err != nil {
		d.Err = fmt.Errorf("%w: evaluating %s at %s: %w", ErrDirectionUnknown, portdir, git.Abbrev(tip), err)
		return d
	}
	m, err := macports.Move(from, to)
	if err != nil {
		d.Err = fmt.Errorf("%w: %w", ErrDirectionUnknown, err)
		return d
	}
	d.Movement = m
	// base's own predicate, and not a dockhand judgment over Backwards and
	// EpochMoved: the version string moved and base would decline to
	// upgrade an install at the old one, which is the downgrade that
	// strands every existing install.
	d.EpochOwed = m.Moved && !m.Upgrades
	return d
}

// askForge is the live half of the forge standing: who upstream is,
// which remote holds this checkout's fork, what this branch's own pull
// request is, and whether an open one already proposes the same change.
//
// EVERY FAILURE LANDS ON Err AND STOPS THE WALK. A forge that would not
// answer one question has not answered the ones after it either, and a
// gatherer that carried on would hand Authorize a fact set where "no
// duplicate" means "the search never ran". The machine refuses on Err
// and the person is advised, which is the split ForgeFacts.Err is
// declared for.
func askForge(ctx context.Context, e Env, f Facts) ForgeFacts {
	out := ForgeFacts{AsOf: f.AsOf, Fresh: true}
	upstream, err := gh.UpstreamRepo(ctx, e.Forge, e.Repo)
	if err != nil {
		out.Err = err
		return out
	}
	remote, owner, err := gh.ForkRemote(ctx, e.Forge, e.Repo, f.Asks.Remote)
	if err != nil {
		out.Err = err
		return out
	}
	out.Upstream, out.ForkRemote, out.ForkOwner = upstream, remote, owner
	// This branch's own pull request, looked up by the FORK OWNER and
	// never by tracking config (D21): a branch --replace just re-minted
	// has none until a push restores it, a fresh clone has none for any
	// branch, and `git branch --unset-upstream` removes it outright.
	pr, found, err := gh.QueryPR(ctx, e.Forge, upstream, owner, f.Branch)
	if err != nil {
		out.Err = err
		return out
	}
	out.Own, out.OwnFound = pr, found
	if f.Asks.noPRCheck(f.Invoker) {
		return out
	}
	port := portName(headline(f.Change).Port, f.Title)
	if port == "" {
		// Nothing to search by. The walk is not run rather than run
		// unbounded, and Asked stays false so the body's checklist box is
		// left unticked rather than vouching for a search nobody made.
		return out
	}
	prs, err := gh.OpenPortPRs(ctx, e.Forge, upstream, port)
	if err != nil {
		out.Err = err
		return out
	}
	out.Asked = true
	for _, other := range prs {
		if found && other.Number == pr.Number {
			// Re-publishing updates that pull request in place; matching
			// against it would refuse the branch for duplicating itself.
			continue
		}
		if strings.EqualFold(strings.TrimSpace(other.Title), strings.TrimSpace(f.Title)) {
			out.Duplicate, out.DuplicateFound = other, true
			// The walk stops at the duplicate, and the advisories for
			// everything ahead of it are kept: the shipped search reported each
			// pull request as it walked past, so those were already said by the
			// time it found this one.
			return out
		}
		out.SamePort = append(out.SamePort, other)
	}
	return out
}

// cachedForge serves what the RECORD holds about this change's
// publication and asks the forge nothing: the number and the URL of the
// row, dated by its last step.
//
// It fabricates no forge state word, and that is deliberate rather than
// lazy. record.Publication carries an Outcome, which is what dockhand
// concluded; gh.PullRequest carries the forge's own spellings, which is
// what the forge said. Filling the second from the first would put a
// sentence in a report that no forge ever uttered — and since Authorize
// refuses facts whose Fresh is false, no gate would ever have caught it.
// What a reader gets is the row, with an AsOf, and Fresh false saying
// which.
func cachedForge(s statestore.State, c record.Change) ForgeFacts {
	out := ForgeFacts{}
	p, ok := latestRow(s, c.ID)
	if !ok {
		// Nothing recorded: `status --no-update` over a change that has
		// never been published. Fresh false and AsOf zero is the honest
		// pair — nobody asked, and there is nothing to serve.
		return out
	}
	out.Own = gh.PullRequest{Number: p.Number, HTMLURL: p.URL}
	out.OwnFound = p.Number != 0
	for _, step := range p.Steps {
		if step.At.After(out.AsOf) {
			out.AsOf = step.At
		}
	}
	return out
}

// latestRow is the publication row a report speaks about: the OPEN one
// where a change has one, and otherwise the newest by id. A change is
// published once and retired once, so the open row is unique; the
// fallback is what a retired change's report reads.
func latestRow(s statestore.State, id record.ChangeID) (record.Publication, bool) {
	var found record.Publication
	ok := false
	for _, key := range slices.Sorted(maps.Keys(s.Publications)) {
		p := s.Publications[key]
		if p.Change != id {
			continue
		}
		if !ok || !p.Outcome.Settled() {
			found, ok = p, true
		}
	}
	return found, ok
}

// Spent is Spend's only constructor: the machine publications the store
// already holds inside MaxWindow, with the instant each of them opened.
//
// WHICH STEPS COUNT. A publication counts when its OpenPR step Finished
// — a pull request was opened — and, by rule 7, when that step is
// Uncertain, because a crash between the push and the forge's answer may
// have opened one and a machine must not spend an allowance it cannot
// account for. A RefreshPR step NEVER counts: an adversarial pass showed
// a dispatcher refreshing every open pull request on every tick spending
// the whole 20/6h allowance on refreshes inside two hours and then
// refusing real publications for the rest of the window. A no-op permit
// never counts either, and cannot: it writes no step at all.
//
// THE WINDOW HERE IS MaxWindow AND NOT THE PACE'S. Gather takes no Pace
// — the pace is Authorize's parameter — so the derivation collects
// everything inside the constant floor Compact keeps machine rows above,
// and Spend.Within applies whatever window the pace actually names. cli
// refuses a --publish-every longer than MaxWindow, so the floor always
// covers the window a decision asks for.
//
// It is pure over one consistent read, beside statestore.State's own
// Owed and Live and run.Count's Open, and it takes a State VALUE rather
// than a *Store so a caller may derive it inside an Amend closure.
func Spent(s statestore.State, now time.Time) Spend {
	out := Spend{counted: true, at: s.At}
	since := now.Add(-MaxWindow)
	for _, key := range slices.Sorted(maps.Keys(s.Publications)) {
		p := s.Publications[key]
		if p.By != record.Machine {
			continue
		}
		for _, step := range p.Steps {
			if step.Kind != record.OpenPR {
				continue
			}
			if step.Phase != record.Finished && step.Phase != record.Uncertain {
				continue
			}
			if step.At.After(since) {
				out.stamps = append(out.stamps, step.At)
			}
		}
	}
	sort.Slice(out.stamps, func(i, j int) bool { return out.stamps[i].Before(out.stamps[j]) })
	return out
}

// portName is the port to search open pull requests by: the one the
// change names, or — for a change whose subjects say nothing — the
// prefix of the title, leaning on the project convention that a title is
// `<port>: <description>`. Empty when neither answers, which is the
// caller's cue to skip the duplicate search rather than run an unbounded
// one.
func portName(recorded, title string) string {
	port := recorded
	if before, _, found := strings.Cut(title, ":"); port == "" && found {
		port = strings.TrimSpace(before)
	}
	return port
}

// intentBump is the plan intent a version bump records — the value
// intent/bump writes into Plan.Intent and the mint copies onto the
// change's headline subject. Spelled here because this is its only
// reader: nothing else asks a change what kind of thing it holds, and a
// constant in the plan package for one reader would be a promise the
// plan does not otherwise make.
const intentBump = "bump"

// bumpVersion is the version a change takes its port to, read from the
// intent and target the planner recorded — or nothing, when the change
// is not a bump. A revision bump's target is a revision and a
// re-derivation's is the word "checksums"; a version comparison against
// either would be arithmetic on the wrong kind of thing, so a
// publication of one has no version to weigh the other pull requests
// against and the advisory states theirs without a comparison.
func bumpVersion(s record.Subject) string {
	if s.Intent != intentBump {
		return ""
	}
	return s.Target
}

// headVersion is the version a pull request takes its port to, read off
// its head branch, with the words that say where it was read from; both
// empty when the branch says nothing dockhand can vouch for.
//
// The one branch name dockhand can read is one it minted. A bump's
// branch is dockhand/<port>-<version> by construction, so cutting the
// namespace and then the port off the front is inverting that
// construction rather than parsing prose. A branch outside the namespace
// is somebody's own naming and is not read; a title is prose and is
// never read.
func headVersion(head, port string) (version, source string) {
	slug, minted := strings.CutPrefix(head, git.BranchNamespace)
	if !minted {
		return "", ""
	}
	target := targetIn(slug, port)
	if !versionTarget(target) {
		return "", ""
	}
	return target, "its branch name " + head
}

// targetIn reads what a slug says a change moves its port to: the
// remainder after the port and a hyphen.
//
// It refuses to guess past what it can justify. A slug that does not
// begin with the port keeps its whole self as the target rather than
// being split at the first hyphen, because splitting blindly would name
// "1" as the target of "jq-1.8.2" the moment the port were empty, and a
// wrong version in an advisory is worse than a coarse one.
func targetIn(slug, port string) string {
	if port == "" {
		return slug
	}
	if rest, ok := strings.CutPrefix(slug, port+"-"); ok {
		return rest
	}
	return slug
}

// versionTarget tells a bump's target from the other intents'. It knows
// their three constructions by name — <port>-rev<N>, <port>-checksums,
// <port>-housekeeping — because they are dockhand's own and there are
// three; anything else a minted slug carries after the port is what a
// bump put there. An empty target is a slug that did not carry the port
// at all, and is nothing.
func versionTarget(target string) bool {
	switch target {
	case "", "checksums", "housekeeping":
		return false
	}
	if n, isRev := strings.CutPrefix(target, "rev"); isRev && n != "" &&
		strings.TrimLeft(n, "0123456789") == "" {
		return false
	}
	return true
}

// samePortText is the advisory for one open pull request on the same
// port, as rich as the facts allow and no richer.
//
// Where the other pull request's version is unknown the note is the fact
// that it exists: declining to guess is the whole point, since the
// sentence is about somebody else's work and a wrong version in it is
// worse than none. Where it is known the note says so and says where it
// was read from, so a reader can weigh the source; and where this
// publication's version is known too, how the two stand — the same,
// theirs newer, theirs older — under MacPorts' own ordering and never
// string order, which would call 1.10 older than 1.9.
func samePortText(pr gh.PullRequest, port, version string) string {
	where := fmt.Sprintf("#%d %q (%s)", pr.Number, pr.Title, pr.HTMLURL)
	theirs, source := headVersion(pr.Head.Ref, port)
	if theirs == "" {
		return "an open PR already touches this port: " + where
	}
	stands := ""
	if version != "" {
		switch cmp := macports.VerCmp(theirs, version); {
		case cmp == 0:
			stands = " — the same version as this publication"
		case cmp > 0:
			stands = " — newer than this publication's " + version
		default:
			stands = " — older than this publication's " + version
		}
	}
	return fmt.Sprintf("an open PR already takes this port to %s%s: %s; version read from %s",
		theirs, stands, where, source)
}
