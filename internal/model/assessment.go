package model

import "time"

// Assessment is what upstream's change means for one port a revision
// changes, against the revision's base (the assessment design, D). It's
// the revision's, not an edit's: whoever changed the port, an update, a
// hand edit, or someone else's pull request, its net change from the base
// is what's assessed. It applies to the revision's files, its base, and
// the policy it was made under, and to nothing else.
type Assessment struct {
	Branch BranchID
	// Tree is the revision's files, and Base the commit they're assessed
	// against, the revision's captured base.
	Tree ObjectID
	Base ObjectID
	// Port is the port assessed, a target of the revision, and Directory
	// its port directory.
	Port      string
	Directory string
	// Comparison is what the assessment found. Its Problem says what kept
	// it from being finished, where something did, which holds as what
	// couldn't be checked does (D4); what it did find stands.
	Comparison UpstreamComparison
	// Policy is the version of the rules it was made under; one made
	// under earlier rules no longer stands.
	Policy int
	At     time.Time
}

// Validate checks the rules every recorded assessment keeps.
func (a Assessment) Validate() error {
	switch {
	case a.Branch == "" || a.Tree == "" || a.Base == "":
		return invalid("an assessment names its branch, its revision's tree, and its base")
	case a.Port == "" || a.Directory == "":
		return invalid("an assessment names its port and its directory")
	case a.Policy < 1:
		return invalid("assessment of %s has no policy version", a.Port)
	}
	return nil
}

// Concern is one thing a submission nobody reviews waits on a person's
// look for (the assessment design, E): where it came from, what raised it
// and what it's about, which are its identity, how the candidate stands on
// it, and its words, which aren't.
type Concern struct {
	Origin                    ConcernOrigin
	Port, Rule, Path, Subject string
	Class                     ConcernClass
	Detail                    string
}

// ConcernOrigin is where a concern came from.
type ConcernOrigin string

const (
	// FromUpstream is an assessment of what upstream's change means for
	// a port.
	FromUpstream ConcernOrigin = "upstream"
	// FromCommitRules is a commit-rule finding.
	FromCommitRules ConcernOrigin = "commit-rules"
	// FromOtherPullRequests is another open pull request for a port, or
	// not knowing whether there is one.
	FromOtherPullRequests ConcernOrigin = "other-pull-requests"
)

// Key identifies a concern: the same concern for two ports, or in two
// places, is two. One raised by no named rule is known by its words too.
func (c Concern) Key() string {
	key := string(c.Origin) + "\x00" + c.Port + "\x00" + c.Rule + "\x00" + c.Path + "\x00" + c.Subject
	if c.Rule == "" {
		key += "\x00" + c.Detail
	}
	return key
}
