package render

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// Stream names which of a run's two streams a line belongs on. A
// `cycle` pass acts as it reports — it settles runs, it deletes a
// branch whose pull request merged, it starts what was deferred — and
// what it says about acting is carried here rather than written where
// it happened, because a pass has two renderings and they disagree
// about where that prose goes. The human report says "discarded …" on
// stdout among the branches; the machine one says it on stderr, where
// it cannot corrupt the document.
type Stream int

const (
	// ToOut is the line's own choice: part of what the verb reports.
	ToOut Stream = iota
	// ToErr is prose about acting — a warning, a fork copy's fate.
	ToErr
)

// Line is one line of a verb's prose with the stream it chose. The text
// carries no newline; the printer adds one.
type Line struct {
	Stream Stream
	Text   string
}

// Prose writes lines to the streams they chose. The machine rendering
// passes the same writer for both, which is how every line lands on
// stderr without any of them having to know that a document is being
// written on the other stream.
func Prose(lines []Line, out, errw io.Writer) {
	for _, l := range lines {
		w := out
		if l.Stream == ToErr {
			w = errw
		}
		fmt.Fprintln(w, l.Text)
	}
}

// PullRequestDocument is a pull request as the forge's own client
// publishes it. `status --json` emits the forge's shape rather than
// one of ours, because a consumer that parses dockhand's report is
// reading about GitHub's object and should not have to learn a second
// spelling of it.
//
// It is an interface, and a narrow one, because this package may not
// import the forge client at all: a renderer that could reach gh could
// query for the answer it is phrasing, and no golden can pin a sentence
// that needs a network. The value arrives already fetched and already
// knowing how to publish itself.
type PullRequestDocument interface{ json.Marshaler }

// Orphan is a running worker no note in this repository accounts for,
// with the owning checkout when the provider's attribution knows it.
// The tags are the wire: this value is published as it stands.
type Orphan struct {
	Name  string `json:"name"`
	Owner string `json:"owner,omitempty"`
}

// Report is what one reconciliation pass found and did: every branch
// observed — the dockhand/* namespace and any other branch carrying a
// verify note — with its verification standing and its pull request's,
// the prose the pass produced while acting, and the workers nothing
// accounts for.
//
// It is the whole of what the two renderings below draw on, and they
// draw on nothing else. That is the point of collecting it: `status`
// and `cycle` (D27) reach one report by one pass and differ only in
// what the pass was allowed to do, so the renderings cannot disagree
// about what a branch is doing — only about what was done to it.
type Report struct {
	// Repository is the checkout the pass ran in. Naming it is the
	// point of the empty report: run from the wrong checkout, "no
	// branches" is true and useless — true and located is actionable.
	Repository string

	// Now is the clock this pass was read against, once, by the caller
	// that did the reading. A running run's line states how long it has
	// been going, and reading the clock inside a renderer would make the
	// one sentence a golden pins depend on when the test ran.
	Now time.Time

	// AsRecorded says the pass polled nothing (`status --no-update`,
	// D27): the standings are the ledger as written, no pull request
	// was checked and no worker audit ran. The report says so once at
	// the top, so that a running run's stale line and a promoted
	// branch's missing pull request are read as unasked rather than as
	// answers.
	AsRecorded bool

	Branches []BranchReport

	// Drain is what starting the deferred runs said — `cycle`'s alone.
	// It is carried rather than printed for the same reason a branch's
	// prose is: the machine rendering routes it to stderr, and both
	// renderings need it to stay behind the branches it followed.
	Drain []Line

	// Reclaimed is what releasing the untracked workers said — `cycle
	// --reclaim-orphans`'s alone, and printed before the drain because
	// that is the order the pass did them in: a slot a reclaim freed is
	// a slot the drain then spent.
	Reclaimed []Line

	// Orphans is the worker audit: the environments the provider is
	// running that no note here accounts for. Supplied by the caller
	// rather than taken by the pass, because the pure read must not ask
	// a provider and the pass does not know which read it is.
	Orphans []Orphan
}

// BranchReport is one branch's row: what was observed of it, what was
// judged about its pull request, and what acting on that judgment did.
//
// Both halves are recorded even when only one will be shown. A branch
// destined to be retired still has its standing read, because the
// machine rendering publishes the standing of a branch it also reports
// as cleaned — and a reader of that document is entitled to know what
// was deleted, not only that something was.
type BranchReport struct {
	Branch string

	// Minted says dockhand made this branch — it lives under the
	// dockhand/* namespace. False is a hand-made branch that carries a
	// verify note and is observed for it (D27's fold-in); nothing here
	// ever deletes one of those, and the document says which is which
	// so a consumer can tell dockhand's own work from work it was only
	// asked to verify.
	Minted bool

	// Tip, Note and Drift are the observation: the branch's tip commit,
	// the settled record covering it, and — when no record does — the
	// drift finding that stands in for one. A nil Note is what makes
	// Drift the whole standing.
	Tip   string
	Note  *record.Record
	Drift string

	// ObserveErr is a standing that could not be read. It becomes the
	// branch's line and the retirement is still stated below it: one
	// unreadable note must not cost the pull request's answer.
	ObserveErr string

	// Retire is the pull request judgment and what acting on it did —
	// or, under `status`, was not asked to do. Both verbs state it with
	// Line, from the same fact, and neither may reach a different one.
	Retire verdict.Reconciliation

	// PR is the forge's object, published as the forge shapes it.
	PR PullRequestDocument

	// PRCreatedAt is when the pull request was opened, and the zero time
	// is a lookup that did not say. It is carried beside PR rather than
	// read out of it because PR is a json.Marshaler and nothing more:
	// this package may not import the forge client, so a timestamp the
	// wording needs has to arrive already parsed.
	//
	// Separate from Retire.PRFact deliberately. A judgment is a function
	// of its facts and reaches the same answer offline, next year, twice;
	// an age is a function of the facts AND the clock, so it belongs on
	// the report the clock was read for and not among the values a
	// verdict weighs.
	PRCreatedAt time.Time

	// Tier is the target port's maintainer tier, read from its Portfile
	// during observation. Empty when nothing could be read — see Tier,
	// where the empty value is a real answer.
	Tier Tier

	// Prose is what retiring this branch said, in the order it said it
	// — `cycle`'s alone, since `status` retires nothing. Ordered, and
	// kept with the branch rather than pooled, because a reader scanning
	// the listing reads "discarded X" as belonging to the X below it —
	// batching every deletion at the end would still print the same
	// lines and tell a different story.
	Prose []Line
}

// standing is the branch's line or lines in the human report: what was
// observed, then what the pull request says — unless the branch was
// cleaned, where the deletion is the whole of what is left to say about
// it.
func (b BranchReport) standing(now time.Time) []string {
	var lines []string
	if b.ObserveErr != "" {
		lines = []string{"error: " + b.ObserveErr}
	} else {
		lines = DescribeChange(b.Note, b.Drift, now)
		// Why the change stopped moving, if it did — a hold, or a newer
		// sibling. Before the proposals, because advice about a change
		// nothing further will happen to is advice to do work twice.
		lines = append(lines, StillnessLines(b.Note, b.Branch)...)
		// What the settlement found beside the verdicts, and the two
		// verbs that answer it. Under the standings rather than among
		// them: a proposal is not a verdict about the change, and a
		// reader scanning for "passed" must not have to read past an
		// advisory to find it.
		lines = append(lines, ProposalLines(b.Note, b.Branch)...)
	}
	extra := b.Retire.Line()
	if b.Retire.Cleaned {
		return []string{extra}
	}
	if extra != "" {
		lines = append(lines, extra)
	}
	// Under the pull request's own line, because they are about it: how
	// far through its review window it is, and the follow-up its age
	// earns. The verdict's line says what the pull request IS and these
	// say how long it has been that way, which is the order a reader
	// needs them in.
	return append(lines, WindowLines(b, now)...)
}

// Text is the human rendering, and it serves both verbs: `dockhand
// status` has no prose and no drain and renders every branch with its
// standing and the worker audit under them; `dockhand cycle` has both
// and renders the same listing with what the pass did among it.
//
// A branch with one line to say shares a line with its name; a branch
// with several is named and its lines indented under it. The prose
// comes first within a branch, because it is about what just happened
// to the branch the reader is about to see — and because it is kept
// with its branch rather than pooled, reordering the listing carries
// each "discarded …" along with the thing it discarded.
//
// The order is attention's, not the namespace's. See Ordered.
func (r Report) Text(out, errw io.Writer) {
	if r.AsRecorded {
		fmt.Fprintln(out, "as recorded — nothing polled (--no-update): running runs not settled, pull requests not checked, no worker audit")
	}
	if len(r.Branches) == 0 {
		fmt.Fprintf(out, "no dockhand branches in %s\n", r.Repository)
	}
	for _, b := range r.Ordered() {
		Prose(b.Prose, out, errw)
		lines := b.standing(r.Now)
		if len(lines) == 1 {
			fmt.Fprintf(out, BranchLine, b.Branch, lines[0])
			continue
		}
		fmt.Fprintln(out, b.Branch)
		for _, l := range lines {
			fmt.Fprintf(out, "  %s\n", l)
		}
	}
	Prose(r.Reclaimed, out, errw)
	Prose(r.Drain, out, errw)
	r.orphans(out)
}

// orphans names running workers no note accounts for: a pre-mint gate
// failure keeps its environment with no branch, another checkout's jobs
// are invisible here, and with a two-guest cap a forgotten worker is an
// expensive kind of quiet.
//
// The width here is a literal and not BranchLine on purpose: the
// subject is a worker name, not a branch, and these lines carry their
// own sentence rather than a standing. They sit under the branch
// listing and happen to share a column today; tuning one column is not
// a reason to move the other.
func (r Report) orphans(out io.Writer) {
	for _, o := range r.Orphans {
		if o.Owner != "" {
			fmt.Fprintf(out, "%-32s worker from %s — `dockhand status` there follows it\n", o.Name, o.Owner)
			continue
		}
		// The remedy names dockhand's own verb (D27) and not the
		// backend's: a kernel renderer that spelled `tart delete` would
		// be wrong the day a worker's provider is not tart.
		fmt.Fprintf(out, "%-32s untracked worker — `dockhand shell %s` reaches it; `dockhand cycle --reclaim-orphans` frees the slot\n", o.Name, o.Name)
	}
}

// statusJSON is the machine rendering of the same observation Text
// performs for `status` — the polling and settling still happen; only
// the telling differs. `cycle` has no machine rendering (D27): it acts,
// and what it did is prose.
//
// It is a type of its own rather than tags on Report because the two
// are different promises: Report is this package's working value and
// may be reshaped whenever the renderings want it reshaped, while these
// names, this order and this set of omitempty are a document somebody's
// script parses. Declaration order is key order, so moving two fields
// here moves the bytes.
type statusJSON struct {
	Repository string `json:"repository"`
	// AsRecorded is `--no-update`'s mark on the document: absent when
	// the pass polled, true when it did not, so a consumer reading a
	// running run or a missing pull request knows whether anybody
	// asked.
	AsRecorded    bool           `json:"as_recorded,omitempty"`
	Branches      []statusBranch `json:"branches"`
	OrphanWorkers []Orphan       `json:"orphan_workers,omitempty"`
	// Exit is the process's own status, said inside the document as
	// well as beside it: a consumer that captured stdout through a pipe
	// and lost $? still knows how the run ended. Last, so every key
	// above it keeps the offset it has always had.
	Exit exitcode.Twin `json:"exit"`
}

type statusBranch struct {
	Branch string `json:"branch"`
	// Minted is never omitted: false is the fold-in's hand-made branch,
	// and a consumer must be able to tell it from dockhand's own.
	Minted bool   `json:"minted"`
	Tip    string `json:"tip,omitempty"`
	// Note is the tip's verdict set, absent when the tip has none, and
	// emitted exactly as stored: its own JSON is the schema, so a
	// consumer reads what a future dockhand would.
	Note *record.Record `json:"note,omitempty"`
	// Drift is the human sentence about an unnoted tip — content
	// identity, commits behind — kept as prose: it is a finding, not a
	// state machine.
	Drift   string              `json:"drift,omitempty"`
	PR      PullRequestDocument `json:"pr,omitempty"`
	PRError string              `json:"pr_error,omitempty"`
	Error   string              `json:"error,omitempty"`
}

// JSON is `dockhand status --json`: the document on out, and every word
// the pass said on err, where it cannot corrupt it. `status` acts on
// nothing (D27), so the prose routed there is the bookkeeping's own
// warnings; the routing stays because a document with a sentence in it
// is a broken document whatever the sentence.
//
// An encoder rather than a marshal: the trailing newline and the
// HTML escaping of a PR title carrying & or < are both part of what has
// always been published.
//
// The exit twin is the caller's to supply, for the reason this package
// takes a clock read rather than reading one: the status a process
// leaves behind is not a function of the report, and a document that
// worked its own out could disagree with $?.
func (r Report) JSON(out, errw io.Writer, exit exitcode.Twin) error {
	// The same order the human report lists in, for the same reason the
	// two renderings draw on one value at all: `status` and `status
	// --json` are one verb told twice, and a consumer that diffs the
	// array against what a person read must not find them disagreeing
	// about which branch wanted attention first. The array's ORDER is not
	// part of the document's contract — the keys are — so ordering it
	// costs a positional consumer nothing it was promised.
	ordered := r.Ordered()
	for _, b := range ordered {
		Prose(b.Prose, errw, errw)
	}
	Prose(r.Reclaimed, errw, errw)
	Prose(r.Drain, errw, errw)
	doc := statusJSON{Repository: r.Repository, AsRecorded: r.AsRecorded, Branches: []statusBranch{}, OrphanWorkers: r.Orphans, Exit: exit}
	for _, b := range ordered {
		doc.Branches = append(doc.Branches, statusBranch{
			Branch:  b.Branch,
			Minted:  b.Minted,
			Tip:     b.Tip,
			Note:    b.Note,
			Drift:   b.Drift,
			PR:      b.PR,
			PRError: b.Retire.Err,
			Error:   b.ObserveErr,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
