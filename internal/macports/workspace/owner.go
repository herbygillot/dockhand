package workspace

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/progress"
)

// Owner is whose Portfiles the work under a context evaluates (#162). Own
// is work that needs no one's consent: the person's own Portfiles, or
// MacPorts' own tree as fetched; Of names the work. Otherwise they are
// another person's, as an adopted pull request's are its author's: Whose
// words them, "@alice's", Of names the work, a branch or "#35174", and
// Refused is why they may not be evaluated, the person not having allowed
// the author's commit; nil where they may.
type Owner struct {
	Whose, Of string
	Refused   error
	Own       bool
}

type ownerKey struct{}

// WithOwner marks a context's work with whose Portfiles it evaluates.
// Every tree staged for evaluation comes through Open or Acquire, the one
// door, which refuses an unmarked context and one whose owner is refused,
// and says once whose another's are otherwise.
func WithOwner(ctx context.Context, owner Owner) context.Context {
	return context.WithValue(ctx, ownerKey{}, owner)
}

// Yours marks a context's work as evaluating the person's own Portfiles,
// of names the work: a branch, or a command's own tree.
func Yours(ctx context.Context, of string) context.Context {
	return WithOwner(ctx, Owner{Own: true, Of: of})
}

// MacPorts marks a context's work as evaluating MacPorts' own tree, its
// master as fetched, as outdated and installed read it.
func MacPorts(ctx context.Context) context.Context {
	return WithOwner(ctx, Owner{Own: true, Of: "MacPorts' tree"})
}

// OwnerOf is the context's owner, false where its work named none.
func OwnerOf(ctx context.Context) (Owner, bool) {
	owner, ok := ctx.Value(ownerKey{}).(Owner)
	return owner, ok
}

// ErrUnowned is work that came to the door naming no owner: a route that
// evaluates without asking whose Portfiles it evaluates, which is
// dockhand's own fault, and refused, so nothing evaluates unasked.
var ErrUnowned = errors.New("dockhand: work that evaluates Portfiles named no owner, so it was refused; this is a fault in dockhand (#162)")

// admit is the door: nil for work that needs no consent, the refusal for
// another's Portfiles they haven't allowed, and for another's they have,
// nil, said once: a Portfile is Tcl that MacPorts runs as the person, on
// the Mac that holds the GitHub login. Work that named no owner is
// refused (ErrUnowned): the door fails closed.
func admit(ctx context.Context) error {
	owner, ok := OwnerOf(ctx)
	switch {
	case !ok:
		return ErrUnowned
	case owner.Own:
		return nil
	case owner.Refused != nil:
		return owner.Refused
	}
	progress.ReportOnce(ctx, "Evaluating %s Portfiles on this Mac, for %s", owner.Whose, owner.Of)
	return nil
}
