package model

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// EditID identifies an authoring command's record.
type EditID string

// EditKind is what an authoring command does: what an update asks the
// editor for, and what its edit records.
type EditKind string

const (
	// EditUpdate moves a port to a new version, with its source metadata.
	EditUpdate EditKind = "update"
	// EditChecksums refreshes a port's distfile checksums for its source.
	EditChecksums EditKind = "checksums"
	// EditRevbump increments a port's revision without changing its
	// version.
	EditRevbump EditKind = "revbump"
	// EditCreate is a new port's first Portfile, which create writes; the
	// editor is never asked for it.
	EditCreate EditKind = "create"
	// EditRename is a port renamed, its Portfile and files at the new
	// name's directory and an obsolete stub at the old one; EditMove a
	// port moved to another category's directory, keeping its name.
	EditRename EditKind = "rename"
	EditMove   EditKind = "move"
)

// EditedFile is one file an authoring command wrote: its blob before and
// after, empty where the file was absent.
type EditedFile struct {
	Path   string
	Before ObjectID
	After  ObjectID
}

// Edit is what an authoring command wrote in a branch's working files. It
// is kept so tidy can tell a port's edits that are exactly dockhand's from
// ones a person changed afterwards, and use the subject dockhand wrote.
type Edit struct {
	ID     EditID
	Branch BranchID
	Kind   EditKind
	Port   string
	// Directory is the port's directory, such as textproc/jq.
	Directory string
	// Subject is the commit subject the edit carries, such as
	// "jq: update to 1.8.1".
	Subject string
	Files   []EditedFile
	At      time.Time
	// Upstream is what comparing the old and new upstream archives found,
	// for an update that compared them; nil when it did not.
	Upstream *UpstreamComparison
	// Release is the release an update chose, as it was found: its forge,
	// repository, tag, and upstream commit, or its distfiles. Nil for
	// other edits.
	Release *Release `json:",omitempty"`
	// Excepted are, for a revision bump, the dependents --except left out
	// of the same rebuild, which the pull request's description lists.
	Excepted []string `json:",omitempty"`
}

// UpstreamComparison is what an update's upstream archives showed: an
// assessment of what upstream's change means for the port (the
// assessment design, C).
type UpstreamComparison struct {
	Changes []UpstreamChange `json:"changes"`
	// Problem says why the archives could not be compared.
	Problem string `json:"problem,omitempty"`
	// Coverage says what the assessment read, and what it set apart and
	// why; none in a comparison recorded before it did.
	Coverage []Coverage `json:"coverage,omitempty"`
	// ProblemKind is the Problem's kind (refusal.Kind), which says whether
	// another try may meet it, a network's failure or a forge's rate
	// limit, which collecting the assessment again tries; a refusal, as a
	// 404, or a port that doesn't evaluate, stands for its files. It's
	// where a Transient flag was (R3-6); one recorded with that flag reads
	// as the network's.
	ProblemKind string `json:"problem_kind,omitempty"`
	// Commit is the commit a Git-fetched port's source was read at, as
	// its git.branch named it when it was assessed; empty for one read
	// from archives, and for one assessed before it was kept.
	Commit string `json:"commit,omitempty"`
}

// UnmarshalJSON reads a comparison as recorded, one recorded with the
// Transient flag that ProblemKind replaced as the network's, which another
// try may meet.
func (c *UpstreamComparison) UnmarshalJSON(data []byte) error {
	type recorded UpstreamComparison
	var read struct {
		recorded
		Transient bool `json:"transient"`
	}
	if err := json.Unmarshal(data, &read); err != nil {
		return err
	}
	*c = UpstreamComparison(read.recorded)
	if read.Transient && c.ProblemKind == "" {
		c.ProblemKind = "network"
	}
	return nil
}

// Held reports whether the comparison holds the update for a person's
// look before a submission nobody reviews, serve's or bump's (Holds).
func (c *UpstreamComparison) Held() bool { return len(c.Holds()) > 0 }

// Holds are why the comparison holds the update: each change a passing
// build can't catch, and, since what couldn't be checked holds too (D4),
// the problem that kept the archives from being compared.
func (c *UpstreamComparison) Holds() []string {
	var holds []string
	for _, concern := range c.Concerns("") {
		holds = append(holds, concern.Detail)
	}
	return holds
}

// Concerns are Holds, typed, each a concern of the port's: each change a
// passing build can't catch, and the problem that kept the archives from
// being compared.
func (c *UpstreamComparison) Concerns(port string) []Concern {
	if c == nil {
		return nil
	}
	var concerns []Concern
	for _, change := range c.Changes {
		if change.Hold {
			concerns = append(concerns, Concern{Origin: FromUpstream, Port: port, Rule: change.Rule, Source: change.Source, Path: change.Path, Subject: change.Subject, Class: change.Class, Detail: change.Message})
		}
	}
	if c.Problem != "" {
		concerns = append(concerns, Concern{Origin: FromUpstream, Port: port, Rule: "not-compared", Class: UnknownBaseline,
			Detail: "the upstream archives couldn't be compared: " + c.Problem})
	}
	return concerns
}

// UpstreamChange is one finding of an assessment: a license file, a build
// file, or a declared dependency that changed; a file the comparison
// couldn't read (kind "unread"); or what a change asks of the port, such
// as a Python requirement its dependency doesn't meet. One that holds is a
// concern: it asks a person's look before a submission nobody reviews.
type UpstreamChange struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Hold    bool   `json:"hold"`
	// Rule is what raised it, and Subject what it's about within its
	// path, such as a dependency's name: with the path, its identity,
	// which its message isn't. Empty in a finding recorded before they
	// were kept.
	Rule    string `json:"rule,omitempty"`
	Subject string `json:"subject,omitempty"`
	// Class says how the candidate stands against the base on it.
	Class ConcernClass `json:"class,omitempty"`
	// Source is the archive it was found in, by its identity from one
	// version to the next (macports.SourceMatch.Identity), where the port
	// fetches more than one: two archives' LICENSE changes are two
	// findings. Empty for a port of one archive.
	Source string `json:"source,omitempty"`
}

// Key identifies a finding: its rule, source, path, and subject, or where
// it was recorded without a rule, its kind, path, and message.
func (c UpstreamChange) Key() string {
	if c.Rule == "" {
		return c.Kind + "\x00" + c.Path + "\x00" + c.Message
	}
	return c.Rule + "\x00" + c.Source + "\x00" + c.Path + "\x00" + c.Subject
}

// ConcernClass is how a candidate stands against its base on a finding.
type ConcernClass string

const (
	// Introduced is new with the candidate, as any change is.
	Introduced ConcernClass = "introduced"
	// Present is the base's too: said, and holding nothing, since an
	// update isn't an audit of everything the port already was.
	Present ConcernClass = "present"
	// UnknownBaseline is where the base couldn't be assessed, which
	// holds as what couldn't be checked does (D4).
	UnknownBaseline ConcernClass = "unknown-baseline"
)

// Coverage is one thing an assessment read, or set apart, and why: a
// file, with the evidence of whether the port's build reads it (Relevance)
// kept apart from what the assessment did with it (Treatment), so a
// policy's setting apart is never mistaken for a proof.
type Coverage struct {
	Path string `json:"path"`
	// Source is the archive Path is in, as a finding's is.
	Source string `json:"source,omitempty"`
	System string `json:"system,omitempty"`
	// Relevance is used, irrelevant, or unknown: whether the port's build
	// is known to read the file.
	Relevance string `json:"relevance"`
	// Treatment is inspected, or set-apart, and Policy the rule that set it
	// apart, with Reason in words.
	Treatment string `json:"treatment"`
	Policy    string `json:"policy,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Validate checks the rules every stored edit keeps.
func (e Edit) Validate() error {
	switch {
	case e.ID == "" || e.Branch == "":
		return invalid("edit %q has no ID or branch", e.ID)
	case !slices.Contains([]EditKind{EditUpdate, EditChecksums, EditRevbump, EditCreate, EditRename, EditMove}, e.Kind):
		return invalid("edit %s has unknown kind %q", e.ID, e.Kind)
	case e.Port == "" || e.Directory == "" || e.Subject == "":
		return invalid("edit %s has no port, directory, or subject", e.ID)
	case len(e.Files) == 0:
		return invalid("edit %s changed no files", e.ID)
	case e.At.IsZero():
		return invalid("edit %s has no time", e.ID)
	}
	for _, file := range e.Files {
		if file.Path == "" || file.Before == file.After {
			return invalid("edit %s records an unchanged or unnamed file", e.ID)
		}
	}
	return nil
}

// CheckpointKind names what rewrote the history a checkpoint keeps.
type CheckpointKind string

const (
	CheckpointTidy   CheckpointKind = "tidy"
	CheckpointRebase CheckpointKind = "rebase"
)

// CheckpointState is how far the change a checkpoint keeps has come.
type CheckpointState string

const (
	// CheckpointPrepared is recorded before its Git change is made.
	CheckpointPrepared CheckpointState = "prepared"
	// CheckpointApplied is a change made: the branch holds After.
	CheckpointApplied CheckpointState = "applied"
	// CheckpointAbandoned is a change that wasn't made; nothing restores it.
	CheckpointAbandoned CheckpointState = "abandoned"
)

// Checkpoint keeps a branch's history from before tidy or rebase rewrote
// it, for restore. Its Git ref, refs/dockhand/checkpoints/<name>, keeps
// the old commits reachable. It is recorded, prepared, before the change
// it keeps is made, and settled, applied or abandoned, after, so a process
// that stops between them leaves a checkpoint the next one can finish.
type Checkpoint struct {
	// Number counts a repository's checkpoints from 1, whatever their kind.
	Number int
	Kind   CheckpointKind
	State  CheckpointState
	Branch BranchID
	// Before is the branch head it replaced, and After the one it wrote.
	Before, After ObjectID
	// BaseBefore and BaseAfter are the master the branch started from
	// before and after: a rebase moves it, and a tidy doesn't.
	BaseBefore, BaseAfter ObjectID
	// Index is the tree the index held before the rewrite, which can hold
	// staged content neither Before nor the working files have. IndexRef
	// keeps it reachable. Empty when nothing was recorded, as for a
	// rebase, which only runs with nothing uncommitted.
	Index      ObjectID
	At         time.Time
	RestoredAt *time.Time
}

// Name is what people type: tidy-3, or rebase-4.
func (c Checkpoint) Name() string { return fmt.Sprintf("%s-%d", c.Kind, c.Number) }

// Ref is the Git ref that keeps the old history.
func (c Checkpoint) Ref() string { return "refs/dockhand/checkpoints/" + c.Name() }

// IndexRef is the Git ref that keeps the replaced index: a commit of its
// tree on Before.
func (c Checkpoint) IndexRef() string { return c.Ref() + "-index" }

// Validate checks the rules every stored checkpoint keeps.
func (c Checkpoint) Validate() error {
	switch {
	case c.Number <= 0 || c.Branch == "":
		return invalid("checkpoint %d has no number or branch", c.Number)
	case c.Kind != CheckpointTidy && c.Kind != CheckpointRebase:
		return invalid("checkpoint %d has unknown kind %q", c.Number, c.Kind)
	case c.State != CheckpointPrepared && c.State != CheckpointApplied && c.State != CheckpointAbandoned:
		return invalid("checkpoint %s has unknown state %q", c.Name(), c.State)
	case c.RestoredAt != nil && c.State != CheckpointApplied:
		return invalid("checkpoint %s is restored, but %s", c.Name(), c.State)
	case c.Before == "" || c.After == "":
		return invalid("checkpoint %s has no heads", c.Name())
	case c.BaseBefore == "" || c.BaseAfter == "":
		return invalid("checkpoint %s has no base", c.Name())
	case c.Kind == CheckpointTidy && c.BaseBefore != c.BaseAfter:
		return invalid("checkpoint %s moved the base, which a tidy doesn't", c.Name())
	case c.At.IsZero():
		return invalid("checkpoint %s has no time", c.Name())
	}
	return nil
}

// Decision is a person's answer to a finding, which a later command reads
// in place of asking again (the direction synthesis's decision record):
// the finding it answers, by its kind and subject, the evidence it rests
// on, and the answer. It lapses when its evidence changes, since it's read
// only for that evidence: an acceptance of a port's failure at one commit
// says nothing of the next (Design v3 §3's publication rule).
type Decision struct {
	// Branch is the branch it was made on; empty for a standing decision,
	// which answers its finding on any branch while its evidence holds.
	Branch  BranchID
	Finding FindingKind
	// Subject is what the finding is about: the port whose failure is
	// accepted.
	Subject string
	// Evidence is what it rests on: the commit a failure was accepted at.
	Evidence string
	Answer   DecisionAnswer
	// Words are the decision's words, whoever wrote them: dockhand's
	// refusal a hand edit answers (Refusal), or, as the person's voice
	// plans, the person's reason. Empty where none were given.
	Words string `json:",omitempty"`
	At    time.Time
}

// FindingKind is the kind of finding a decision answers.
type FindingKind string

const (
	// FindingFailure is a port's failure in a check: a failed revision-only
	// target or extra, or a changed port whose base fails it alike.
	FindingFailure FindingKind = "failure"
	// FindingEnvironmentFailure is a dependency failing in an environment,
	// whatever port needs it: Monterey's Xcode image refusing lang/gcc15 at
	// pre-fetch, so that anything pulling in libgcc15 fails there, master
	// included (the person's choice, 2026-10-18). Its decision is standing,
	// its subject the dependency, and its evidence the dependency's
	// version on the image (EnvironmentEvidence).
	FindingEnvironmentFailure FindingKind = "environment-failure"
	// FindingUndeclaredLink is a port's build linking a library from a
	// port it doesn't declare in depends_lib (v0.5.0 part 6), its subject
	// "<port> links <port>" (evidence.LinkFinding.Subject). A notice: it
	// holds nothing, and a decision may say why it's fine, as a bundled
	// copy.
	FindingUndeclaredLink FindingKind = "undeclared-link"
	// FindingHandEdit is an update dockhand refused, whose best attempt it
	// wrote for the person to finish (update --hand-edit, Then's item 3):
	// its subject the port's directory, its evidence the digest of the
	// attempted files, its words the refusal (HandRefusal), and its answer
	// ByHand.
	FindingHandEdit FindingKind = "hand-edit"
	// FindingConcern is a concern that holds a submission nobody looks
	// over, one finding the person answered with their reason (the
	// person's voice, Then's item 13): its subject the concern's
	// (ConcernSubject), its evidence the branch's files, the tree it was
	// decided on, its words the reason, and its answer Accepted,
	// "acceptable because…", or Waiting, "awaiting…".
	FindingConcern FindingKind = "concern"
)

// ConcernSubject is what a decision on a concern names: its port and
// rule, as "jq license-changed", one decision for each; for a moving
// source, the commits too, abbreviated, as "jq source-moved
// 1a2b3c4..5d6e7f8", so a later move is a new concern.
func ConcernSubject(c Concern) string {
	subject := strings.TrimSpace(c.Port + " " + c.Rule)
	if checked, current, ok := strings.Cut(c.Subject, ".."); ok && MovingSource(c.Rule) {
		subject += " " + ObjectID(checked).Short() + ".." + ObjectID(current).Short()
	}
	return subject
}

// The rules of findings about which commit a Git-fetched port's source
// is, outside its files: where its git.branch, or the tag a GitHub
// archive is fetched by, points now against what the check planned; and
// the commit the update chose, or the assessment read, against it.
const (
	RuleSourceMoved     = "source-moved"
	RuleTagMoved        = "tag-moved"
	RuleReleaseMoved    = "release-moved"
	RuleAssessmentMoved = "assessment-moved"
)

// MovingSource says whether a rule's finding is about which commit a
// port's source is, its subject the two commits it compares
// (MovedSubject). A decision on one holds for those commits, where a
// decision on any other holds while the port's directory does: the
// source can move with the port's files as they were, and an acceptance
// of the move to B answered a later move to C (Codex's workflow review
// of 2ad896d2, D1; decisions.md).
func MovingSource(rule string) bool {
	return rule == RuleSourceMoved || rule == RuleTagMoved || rule == RuleReleaseMoved || rule == RuleAssessmentMoved
}

// MovedSubject is a moving source's concern's subject, the two commits it
// compares, from..to: the commit checked and the one current, or the one
// the update prepared or the assessment read and the one checked.
func MovedSubject(from, to ObjectID) string {
	return string(from) + ".." + string(to)
}

// EnvironmentEvidence is what a standing acceptance of a dependency's
// failure rests on: the dependency's version and revision as the tree has
// them, and the identity of the image it failed on. Either changing lapses
// it.
func EnvironmentEvidence(dependency FailedDependency, identity string) string {
	return dependency.Version + " on " + identity
}

// DecisionAnswer is how a person answered a finding.
type DecisionAnswer string

// Accepted takes a finding as it is: a failure that isn't the branch's
// doing, submitted with the failure disclosed.
const Accepted DecisionAnswer = "accepted"

// ByHand takes an edit dockhand refused as the person's own to make, from
// dockhand's attempt (FindingHandEdit).
const ByHand DecisionAnswer = "by-hand"

// Waiting leaves a concern for later, awaiting what its words say, as
// upstream's answer: it blocks the submission until decided otherwise
// (FindingConcern).
const Waiting DecisionAnswer = "waiting"

// Validate checks the rules every stored decision keeps.
func (d Decision) Validate() error {
	switch {
	case d.Subject == "" || d.Evidence == "" || d.At.IsZero():
		return invalid("decision on %q is incomplete", d.Subject)
	case d.Finding != FindingFailure && d.Finding != FindingEnvironmentFailure && d.Finding != FindingHandEdit && d.Finding != FindingConcern:
		return invalid("decision on %s answers an unknown kind of finding %q", d.Subject, d.Finding)
	case d.Finding == FindingEnvironmentFailure && d.Branch != "":
		return invalid("a dependency's failure in an environment is accepted standing, on no branch: %s", d.Subject)
	case d.Finding == FindingHandEdit && (d.Branch == "" || d.Answer != ByHand || d.Words == ""):
		return invalid("a hand edit of %s is taken by hand on a branch, with dockhand's refusal", d.Subject)
	case d.Finding == FindingConcern && (d.Branch == "" || d.Words == "" || d.Answer != Accepted && d.Answer != Waiting):
		return invalid("a decision on %s is a branch's, accepted or waiting, with its reason", d.Subject)
	case d.Finding != FindingHandEdit && d.Finding != FindingConcern && d.Answer != Accepted:
		return invalid("decision on %s has an unknown answer %q", d.Subject, d.Answer)
	}
	return nil
}

// Review is one review dockhand made of a pull request: what it found at
// which head, and whether it was posted (Design v3 §6.11). The next review
// of the same pull request reads it to say which findings are resolved.
type Review struct {
	// Repository is the pull request's base repository, such as
	// macports/macports-ports.
	Repository string
	Number     int
	Head       ObjectID
	Findings   []ReviewFinding
	// Posted is how it was posted: "comment", "request-changes", or ""
	// when it was not.
	Posted string
	At     time.Time
}

// ReviewFinding is one finding of a review, as it was reported.
type ReviewFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Where    string `json:"where,omitempty"`
	Message  string `json:"message"`
}

// Validate checks the rules every stored review keeps.
func (r Review) Validate() error {
	switch {
	case r.Repository == "" || r.Number <= 0 || r.Head == "" || r.At.IsZero():
		return invalid("review of %s#%d is incomplete", r.Repository, r.Number)
	case r.Posted != "" && r.Posted != "comment" && r.Posted != "request-changes":
		return invalid("review of %s#%d was posted as %q", r.Repository, r.Number, r.Posted)
	}
	return nil
}
