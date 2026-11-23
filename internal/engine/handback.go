package engine

import (
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// The hand-back (#195, amending "none is copied back", pending the
// person's word on architecture's card): a publication that took a
// stage's change request and delivered nothing gives the submission's
// asks it took back to that request, where the request's ID is still the
// one it took, so the next submission is a new attempt under the request
// with what it asked. A request changed since keeps its own asks; a
// person's own submit took none and hands nothing back; a publication
// waiting for a person's answer keeps its record, and nothing moves. Kept
// to these two functions, so keeping the page as adopted is deleting them
// and their two calls.

// handBack gives a publication that ends without delivering, refused,
// its Asked back to the request it took, in tx.
func handBack(tx store.Tx, id model.BranchID, record model.Publication) error {
	if record.RequestID == "" {
		return nil
	}
	branch, err := tx.Branch(id)
	if err != nil || branch.Request == nil || branch.Request.ID != record.RequestID {
		return err
	}
	request := *branch.Request
	request.Publish = askedBack(request.Publish, record.Asked)
	return tx.SetRequest(id, &request)
}

// askedBack is a request's submission asks with a publication's put back
// where the request gives none: each as taken, a false given included; a
// publication recorded before its asks kept their given-ness gives back
// what was set, as given.
func askedBack(publish model.PublishAsk, asked model.Asked) model.PublishAsk {
	back := func(field *model.Flag[bool], taken model.Flag[bool]) {
		switch {
		case field.Given:
		case taken.Given:
			*field = taken
		case taken.Value:
			*field = model.Set(true)
		}
	}
	back(&publish.Draft, asked.Draft)
	back(&publish.NoCheck, asked.NoCheck)
	back(&publish.SkipNotification, asked.SkipNotification)
	switch {
	case publish.Type.Given:
	case asked.Types.Given:
		publish.Type = asked.Types
	case len(asked.Types.Value) > 0:
		publish.Type = model.Set(asked.Types.Value)
	}
	return publish
}

// takenAsks are the submission's asks a stage plans under: the request's,
// with those an undelivered publication of the branch took from this same
// request, which a submission for another commit replaces (recordPublication).
func takenAsks(request model.ChangeRequest, branch model.Branch) model.PublishAsk {
	taken := branch.Publication
	if taken == nil || taken.RequestID == "" || taken.RequestID != request.ID || taken.Number != 0 {
		return request.Publish
	}
	return askedBack(request.Publish, taken.Asked)
}
