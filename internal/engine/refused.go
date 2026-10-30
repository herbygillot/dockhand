package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// refusedUpdate is why an update didn't go, composed once for every way
// the update sequence stops, where author.go held three composers (R3-38):
// what stopped it, what was kept as it was, from what the request and the
// update say was started, and, for an edit dockhand can't make, the steps
// that make it by hand. A hold before the edit is the command's to say.
func refusedUpdate(err error, request UpdateRequest, update Update) error {
	if err == nil {
		return nil
	}
	started := request.Started || update.Started
	branch := request.Branch.Name
	if update.Started || update.RenamedFrom != "" {
		branch = update.Branch.Name
	}
	if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrFidelity) {
		return byHand(err, request, branch, started)
	}
	if uncertain := new(UncertainRelease); errors.As(err, &uncertain) {
		return uncertainRelease(request, uncertain.SetAside, branch, started)
	}
	if ahead := new(AheadRelease); errors.As(err, &ahead) {
		return aheadRelease(request, *ahead, branch, started)
	}
	if !started || errors.As(err, new(*HeldBeforeEdit)) {
		return err
	}
	// An update that failed after its edit, as an obsolete stub's
	// following did, says the edit it kept: "nothing changed" was said of
	// a worktree holding terraform-1.16's (field testing at a3c43ef7).
	kept := branch + ", with nothing changed"
	if len(update.Files) > 0 {
		kept = fmt.Sprintf("%s, with uncommitted changes to %s", branch, strings.Join(update.Files, ", "))
	}
	refused := refusal.Wrap(refusal.KindOf(err), err, "%s", err).With(kept)
	if r := refusal.Of(err); r != nil {
		refused = refused.Then(r.Next...)
	}
	return refused
}

// byHand is an edit dockhand can't make by itself, why, and the steps
// that make it by hand (Design v3 §6.3), each from what refused it, by its
// type: a PortGroup's checksums, checksums computed but not writable, a
// checksum refresh, a version whose checksums can't be found to edit, or
// any other. Advice is given only where it can work: checksums dockhand
// can't find in the Portfile to edit, it can't refresh either, and prints
// instead. A plan changes nothing, so it keeps nothing.
func byHand(err error, request UpdateRequest, branch string, started bool) error {
	port := request.Port
	reason := refusal.WordsOf(err)
	unlocated := new(Unlocated)
	if !errors.As(err, &unlocated) {
		unlocated = nil
	} else {
		reason = unlocated.Error()
	}
	what := port
	if request.Action == model.EditChecksums {
		what = "the checksums for " + port
	}
	words := fmt.Sprintf("Could not update %s automatically: %s", what, reason)
	kept := ""
	switch {
	case request.Plan:
	case started:
		kept = branch + ", with nothing changed"
	case request.Start != nil && !request.HandBranch():
		words += "\nNo branch was started."
	default:
		kept = "the branch, unchanged"
	}
	toWrite := new(ChecksumsToWrite)
	if !errors.As(err, &toWrite) {
		toWrite = nil
	}
	edit := refusal.Next{Verb: refusal.VerbEdit, Args: []string{port}}
	var steps []refusal.Next
	switch {
	case errors.As(err, new(*portfile.InPortGroup)):
		// A PortGroup's own code sets them: it's never the Portfile's
		// edit, and what's said is whether they agree with the archives.
		switch {
		case toWrite != nil && toWrite.Agree:
			words += "\nThe checksums there match the archives as they are now."
		case toWrite != nil:
			words += "\nThe archives as they are now have these, which differ from those there:\n    " + indented(portfile.ChecksumsBlock(toWrite.Checksums))
		}
	case request.Action == model.EditChecksums && toWrite != nil:
		steps = []refusal.Next{{External: "use these checksums in the Portfile:\n    " + indented(portfile.ChecksumsBlock(toWrite.Checksums))}, edit}
	case request.Action == model.EditChecksums:
		steps = []refusal.Next{{External: fmt.Sprintf("run port checksum %s, then copy the reported checksums into the Portfile", port)}, edit}
	case unlocated != nil:
		steps = []refusal.Next{{External: fmt.Sprintf("edit the version, then run dockhand checksums %s to print the checksums to copy into the Portfile", port)}, edit}
	default:
		// checksums meets what stopped the update, evaluating the same
		// Portfile, so it isn't named: git's and qemu's refusals each sent
		// the person to a checksums that refused them again (the hugo
		// exercise, field testing). A step names only what would be
		// accepted (the release bar's rule).
		steps = []refusal.Next{{External: fmt.Sprintf("edit the version, run port checksum %s, then copy the reported checksums into the Portfile", port)}, edit}
	}
	// An attended version update can have dockhand's attempt written, for
	// the person to finish (Then's item 3); serve and bump never take it.
	if request.Action == model.EditUpdate && !request.Unattended && !request.HandEdit && !request.Plan && len(steps) > 0 {
		attempt := strings.Join(slices.DeleteFunc([]string{"dockhand update", port, request.Version, "--hand-edit"}, func(s string) bool { return s == "" }), " ")
		steps = append(steps, refusal.Next{External: "or " + attempt + ", which writes dockhand's attempt for you to finish by hand"})
	}
	return refusal.Wrap(refusal.Unsupported, err, "%s", words).With(kept).Then(steps...)
}

// indented is a block's lines after its first, indented as its first is
// where it's said.
func indented(block string) string {
	return strings.ReplaceAll(block, "\n", "\n    ")
}

// ErrReleaseUncertain marks an update to the newest release that couldn't
// tell which that is (uncertainRelease, aheadRelease): nothing was
// changed, and a branch that carries an update already goes on as it is
// (ship, after dolt's push failed: run again, its v040.15 tag compared
// newer and stopped it short of the check that had passed).
var ErrReleaseUncertain = errors.New("the newest release is uncertain")

// uncertainRelease is an update to the newest release that found none to
// choose: what compares newest was set aside, since its tag's commit is
// older than the port's own, and nothing newer is beyond it. Whether it's
// a release is a person's call, so it waits for a look, and names the
// update that takes it: bump's, where nobody looks over it.
func uncertainRelease(request UpdateRequest, aside []SetAside, branch string, started bool) error {
	verb := "update"
	if request.Unattended {
		verb = "bump"
	}
	kept := ""
	if started {
		kept = branch + ", with nothing changed"
	}
	// One that isn't a release, as certgraph's tag on an older commit
	// isn't, is better left out by the port's own livecheck than updated
	// to "after a look" (field testing at c97bca63).
	return refusal.Wrap(refusal.Attention, ErrReleaseUncertain, "can't tell whether %s is current, so nothing was changed: %s", request.Port, SetAsideWords(aside)).
		With(kept).
		Then(refusal.Next{External: fmt.Sprintf("if %s is a release: dockhand %s %s %s", aside[0].Tag, verb, request.Port, aside[0].Source)},
			refusal.Next{External: fmt.Sprintf("if it isn't, as an old tag spelled oddly: a livecheck.regex in the Portfile that leaves %s out keeps it out of later looks", aside[0].Tag)})
}

// aheadRelease is an update to the newest release of a port ahead of
// every version upstream lists, its own tag not among them: the
// repository may have moved, as mergestat's CLI did, so the release to
// take is a person's to name.
func aheadRelease(request UpdateRequest, ahead AheadRelease, branch string, started bool) error {
	verb := "update"
	if request.Unattended {
		verb = "bump"
	}
	kept := ""
	if started {
		kept = branch + ", with nothing changed"
	}
	return refusal.Wrap(refusal.Attention, ErrReleaseUncertain, "can't tell whether %s is current, so nothing was changed: %s", request.Port, AheadWords(ahead.Current, ahead.Tag)).
		With(kept).
		Then(refusal.Next{External: fmt.Sprintf("with the version to take: dockhand %s %s <version>", verb, request.Port)})
}

// AheadWords say why a port ahead of upstream's tags isn't current.
func AheadWords(current, tag string) string {
	return fmt.Sprintf("its %s isn't among upstream's tags; the newest is %s; the repository may have moved", current, tag)
}

// SetAsideWords say why a port's newest release is uncertain: what was set
// aside, and whose commit it predates.
func SetAsideWords(aside []SetAside) string {
	if len(aside) == 1 {
		return fmt.Sprintf("%s compares newer, but its commit is older than %s's", aside[0].Tag, aside[0].Predates)
	}
	var tags []string
	for _, version := range aside {
		tags = append(tags, version.Tag)
	}
	return fmt.Sprintf("%s compare newer, but their commits are older than %s's", strings.Join(tags, ", "), aside[0].Predates)
}
