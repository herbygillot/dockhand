package workspace

import (
	"context"

	"github.com/herbygillot/dockhand/internal/progress"
)

// Owner is whose Portfiles the work under a context evaluates, where they
// are another person's, as an adopted pull request's are its author's
// (#162): Whose words them, "@alice's", and Of names the work, a branch or
// "#35174". Refused is why they may not be evaluated, the person not
// having allowed the author's commit; nil where they may.
type Owner struct {
	Whose, Of string
	Refused   error
}

type ownerKey struct{}

// WithOwner marks a context's work as evaluating another person's
// Portfiles. Every tree staged for evaluation comes through Open or
// Acquire, the one door, which refuses a marked context whose owner is
// refused, and says once whose Portfiles it evaluates otherwise.
func WithOwner(ctx context.Context, owner Owner) context.Context {
	return context.WithValue(ctx, ownerKey{}, owner)
}

// OwnerOf is the context's owner, false where its work is the person's
// own.
func OwnerOf(ctx context.Context) (Owner, bool) {
	owner, ok := ctx.Value(ownerKey{}).(Owner)
	return owner, ok
}

// admit is the door: nil for the person's own Portfiles, the refusal for
// another's they haven't allowed, and for another's they have, nil, said
// once: a Portfile is Tcl that MacPorts runs as the person, on the Mac
// that holds the GitHub login.
func admit(ctx context.Context) error {
	owner, ok := OwnerOf(ctx)
	if !ok {
		return nil
	}
	if owner.Refused != nil {
		return owner.Refused
	}
	progress.ReportOnce(ctx, "Evaluating %s Portfiles on this Mac, for %s", owner.Whose, owner.Of)
	return nil
}
