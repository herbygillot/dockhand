package refusal

// Verb is the dockhand command a Next names, which the command line
// renders with its flags: the one vocabulary of them, which the command's
// test holds to rendering, and its help reference to existing.
type Verb string

const (
	VerbStatus  Verb = "status"
	VerbRefresh Verb = "status --refresh"
	VerbAdopt   Verb = "adopt"
	VerbWait    Verb = "wait"
	VerbSubmit  Verb = "submit"
	VerbEdit    Verb = "edit"
	VerbArchive Verb = "archive"
	VerbCheck   Verb = "check"
	VerbLogs    Verb = "logs"
	// VerbExplain says why a check or a branch is where it is.
	VerbExplain Verb = "explain"
	VerbTidy    Verb = "tidy"
	VerbSetup   Verb = "setup"
	// VerbOpenChecks opens a pull request's CI checks in the browser, its
	// Args the repository and the number.
	VerbOpenChecks Verb = "open-checks"
	// VerbRerunCI runs a branch's pull request's failed CI jobs again,
	// through the person's login; the command line spells it.
	VerbRerunCI Verb = "rerun-ci"
	// VerbStart starts a branch; with --from, a merged branch's commits
	// past its merge, picked onto fresh master.
	VerbStart Verb = "start"
	// VerbRebase moves a branch onto master as fetched now.
	VerbRebase Verb = "rebase"
	// VerbDecide answers one concern that holds a branch, with the
	// person's reason (the person's voice).
	VerbDecide Verb = "decide"
)

// Verbs are every verb a Next names, for the command line to render each.
var Verbs = []Verb{VerbStatus, VerbRefresh, VerbAdopt, VerbWait, VerbSubmit, VerbEdit, VerbArchive, VerbCheck, VerbLogs, VerbExplain, VerbTidy, VerbSetup, VerbOpenChecks, VerbRerunCI, VerbStart, VerbRebase, VerbDecide}

// After is what must change before an action refused can be asked again
// as it was: a person's look at what holds it, a check finishing, or a
// person's edit. Empty where nothing need change but the command run.
type After string

const (
	// AfterLook is a person's look at what holds the branch.
	AfterLook After = "look"
	// AfterCheck is the branch's check finishing.
	AfterCheck After = "check"
	// AfterEdit is a person's edit of the branch's files.
	AfterEdit After = "edit"
	// AfterFork is a fork of MacPorts' repository on GitHub, added as a
	// Git remote, which a submission pushes to.
	AfterFork After = "fork"
)

// Next is what moves something on: a dockhand command, the branch it's
// for, and its arguments, and what must change first, where the command is
// the one refused; or, where the remedy isn't a dockhand command, it in
// words (External), as "sudo port upgrade tart".
type Next struct {
	Verb   Verb
	Branch string
	// Args are the command's own, as the verb takes them.
	Args  []string
	After After
	// External is a remedy that isn't a dockhand command, where Verb is
	// empty.
	External string
}

// Advances reports whether a refusal's Next moves on from the action it
// refuses, named by its verb: another verb, or the same one once something
// has changed; never the same action asked again with nothing to wait for.
func (n Next) Advances(refused string) bool {
	return n.Verb != Verb(refused) || n.After != ""
}
