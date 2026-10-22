package branchstate

// Verb is the dockhand command a Next names, which the command line
// renders with its flags.
type Verb string

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

// Next is what moves a branch on: a command, the branch it's for, and its
// arguments, and what must change first, where the command is the one
// refused, as "once it's fine: dockhand submit" is the same verb, gated
// (architecture, 2026-10-22). It's the verdict's, typed, where each command
// wrote its own line.
type Next struct {
	Verb   Verb
	Branch string
	// Args are the command's own, as the verb takes them.
	Args  []string
	After After
}

// Advances reports whether a refusal's Next moves on from the action it
// refuses: another verb, or the same one once something has changed; never
// the same action asked again with nothing to wait for.
func (n Next) Advances(refused Action) bool {
	return n.Verb != Verb(refused) || n.After != ""
}
