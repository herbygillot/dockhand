package model

import "time"

// Publication is a submission of one commit that dockhand has begun and
// has not yet seen finished (docs/designs/publication-record.md, #191).
// It is recorded before the first remote effect, names its whole
// destination, and owes its steps in order until a read shows each done.
// A branch has one at a time, beside its PullRequest, which is what
// dockhand knows GitHub has from it.
type Publication struct {
	// ID is random; every write of the publication checks it is still the
	// branch's.
	ID string
	// Commit is the commit being published; every step is for it.
	Commit ObjectID
	At     time.Time
	// By is the GitHub login it was recorded under: who, never a
	// permission. Served is serve's recording it.
	By     string
	Served bool `json:",omitempty"`
	// Head is owner/repo:branch it pushes to, Base owner/repo:branch the
	// pull request is asked of, and Number the pull request it updates, 0
	// where it sets out to open one.
	Head, Base string
	Number     int `json:",omitempty"`
	// Earlier is the newest pull request the plan found for Head and Base
	// that wasn't open, 0 where none: only one numbered above it settles
	// open.
	Earlier int `json:",omitempty"`
	// Expected is the head branch as the plan saw it, empty where it
	// didn't exist.
	Expected ObjectID `json:",omitempty"`
	// Title and Body are what it set out to write, and Written dockhand's
	// parts of Body; all three empty where it writes none.
	Title   string `json:",omitempty"`
	Body    string `json:",omitempty"`
	Written string `json:",omitempty"`
	// Asked is what the person asked of the submission, and Review the
	// reviewers to ask again, where the push calls for it.
	Asked  Asked      `json:",omitzero"`
	Review *ReviewAsk `json:",omitempty"`
	// Owes are the steps still owed, in order.
	Owes []PublicationStep
}

// PublicationStep is one of a publication's steps.
type PublicationStep string

const (
	StepDraft     PublicationStep = "draft"
	StepPush      PublicationStep = "push"
	StepOpen      PublicationStep = "open"
	StepEdit      PublicationStep = "edit"
	StepReady     PublicationStep = "ready"
	StepRerequest PublicationStep = "rerequest"
)

// PublicationSteps are every step, in the order they are made.
var PublicationSteps = []PublicationStep{StepDraft, StepPush, StepOpen, StepEdit, StepReady, StepRerequest}

// Asked is what a person chose for a submission, kept so that finishing
// it needs none of it typed again.
type Asked struct {
	Draft, NoCheck, SkipNotification, Rewrite bool
	Types                                     []string `json:",omitempty"`
}

// ReviewAsk is "ask them to review again?", its answer, and what became
// of each request.
type ReviewAsk struct {
	Answer    ReviewAnswer
	Reviewers []Reviewer
}

// ReviewAnswer is the person's answer about the reviewers: yes, no, or not
// asked yet.
type ReviewAnswer string

const (
	ReviewYes      ReviewAnswer = "yes"
	ReviewNo       ReviewAnswer = "no"
	ReviewNotAsked ReviewAnswer = ""
)

// Reviewer is one login to ask, and how far asking got.
type Reviewer struct {
	Login string
	// Reviewed is GitHub's time of this login's own latest review when
	// the question was put: a newer review makes the answer another
	// review's.
	Reviewed time.Time
	Sent     Sent
	// Why are GitHub's words, where it refused.
	Why string `json:",omitempty"`
}

// Sent is how far a request that can't be read back got: a mark recorded
// before the call, cleared only by GitHub's own answer.
type Sent string

const (
	NotSent      Sent = ""
	MaySent      Sent = "may-have-been-sent"
	SentRequest  Sent = "sent"
	RefusedToAsk Sent = "refused"
)

// Provenance is how dockhand came to know a pull request: GitHub's answer
// to its own create, or a read that found it. Empty on a record from
// before the publication, which keeps the rights it has.
type Provenance string

const (
	Opened Provenance = "opened"
	Found  Provenance = "found"
)
