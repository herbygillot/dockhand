package model

import "time"

// ServeOpening is one row of serve's count: a pull request serve opened,
// may have opened, or holds a place to open (docs/designs/
// publication-record.md, "serve's count"). It isn't deleted with a branch,
// which is what let #155's opening leave the count.
type ServeOpening struct {
	// ID is the row's, 0 before it is recorded.
	ID         int64
	Base, Head string
	// After is the newest pull request known for Head and Base when the
	// row was written, 0 where none: only one numbered above it is the
	// row's.
	After int
	// Holding is a place held for a publication that still owes open.
	Holding bool
	// Tried is when a create was first marked, written before it is sent;
	// Unanswered a create of it that got no answer.
	Tried      time.Time
	Unanswered bool
	// Number and Opened are the pull request found for it, and GitHub's
	// time for its creation.
	Number int
	Opened time.Time
	// Gone is when its head was first seen gone from the fork.
	Gone time.Time
}

// Counts reports whether a row counts against the day that starts at day:
// a place held counts every day while its publication owes open; a create
// that may have landed counts every day, since no length of time settles
// it; an opening counts on the day GitHub says it was created.
func (o ServeOpening) Counts(day time.Time) bool {
	next := day.Add(24 * time.Hour)
	switch {
	case !o.Opened.IsZero():
		return !o.Opened.Before(day) && o.Opened.Before(next)
	case !o.Tried.IsZero():
		return true
	}
	return o.Holding
}

// Since is when a row took its place: its create, else its opening.
func (o ServeOpening) Since() time.Time {
	if !o.Tried.IsZero() {
		return o.Tried
	}
	return o.Opened
}

// FoundFor charges a pull request found for a row that was tried: one
// numbered above After, with GitHub's creation time, is its opening; one
// at or below it was there before the create and says nothing of it, nor
// does one with no creation time (case AN).
func (o ServeOpening) FoundFor(number int, created time.Time) (ServeOpening, bool) {
	if number <= o.After || created.IsZero() {
		return o, false
	}
	o.Number, o.Opened, o.Gone, o.Holding = number, created, time.Time{}, false
	return o, true
}

// Refused reports whether GitHub refusing a create releases the row: only
// where it was the only create the row ever sent, so nothing of it is
// left unanswered (case AK).
func (o ServeOpening) Refused() bool { return !o.Unanswered }

// GoneLook is a look at the row's head on the fork: present, the mark is
// cleared; gone at a first look, when is written down; gone again at a
// look at least ten minutes after, with no pull request found above
// After, the row is released. That is an escape the person chose, not a
// proof (the design's choice 8, case AR).
func (o ServeOpening) GoneLook(present bool, now time.Time) (ServeOpening, bool) {
	switch {
	case present:
		o.Gone = time.Time{}
		return o, false
	case o.Gone.IsZero():
		o.Gone = now
		return o, false
	}
	return o, now.Sub(o.Gone) >= goneLookApart
}

// goneLookApart is how far apart the two looks that release a row whose
// head left the fork are: room, not a bound.
const goneLookApart = 10 * time.Minute
