package courtesy

// Outcome is what one request to a host amounted to, said by the seam
// that made it.
//
// It exists because the alternative is to hand the pacer an error and
// ask it to work out what happened, and that is a guess dressed as a
// test. The words on an error arrive through three different tools'
// formatting, none of which promises a status code, so the guess is a
// substring match — and the substring match got the most important
// case exactly backwards. A 304 is the cheapest and most successful
// answer a conditional request can get; `gh api` reports it as a
// non-zero exit because it is not a 2xx; so the one witness that was
// working perfectly was the one being counted against its host, three
// of them in a row from a warm cache walling the forge for a quarter
// of an hour.
//
// The fix is not a better match. It is that the classification is made
// where the facts still exist — beside the status line, the exit code
// and the response, before any of it has been flattened into a
// sentence — and travels here as a value. What the pacer does with
// each of these is policy and lives in Note; what each of them MEANS
// is stated below and is not negotiable per call site.
type Outcome int

const (
	// Unanswered is the zero value on purpose. A seam that classified
	// nothing has told the pacer nothing, and the safe reading of
	// silence is a request that did not work — the reading that
	// eventually walls a host rather than the one that asks it
	// forever. It is also the honest name for the failure a forge does
	// not word: DNS that stopped resolving, a captive network, an
	// outage. Strikes.
	Unanswered Outcome = iota
	// Answered: the host was asked and answered. Clears the streak.
	Answered
	// NotModified: the host was asked, and answered that nothing has
	// changed since the validator it issued. It is a SUCCESS, and
	// saying so is the whole of D5: a conditional request that gets
	// what it asked for is the cache working, not the host failing,
	// and it clears the streak exactly as Answered does.
	NotModified
	// Refused: the host told dockhand to go away — a rate limit, an
	// abuse warning, a 403. Walls, so that every other port on the
	// same host stops asking too.
	Refused
	// Ours: the request ended on dockhand's own clock — a context
	// cancelled, a per-target deadline, or a request the pacer never
	// let out of the queue. Neither a strike nor a clear, because the
	// host said nothing and holding it responsible for a Ctrl-C would
	// let one interrupt wall the tree.
	Ours
)

func (o Outcome) String() string {
	switch o {
	case Unanswered:
		return "unanswered"
	case Answered:
		return "answered"
	case NotModified:
		return "not modified"
	case Refused:
		return "refused"
	case Ours:
		return "ours"
	}
	return "unknown outcome"
}

// Note applies one request's outcome to the host's budget: a success
// or a revalidation clears the streak, a refusal raises the wall, an
// unreadable failure counts a strike, and dockhand's own interruption
// counts nothing.
//
// cause is what the request came back with, kept only so that a wall
// can quote the refusal that raised it; it is never read to decide
// what happened, which is the point of taking an Outcome at all.
//
// It is one method rather than three call sites choosing among
// Cleared, Wall and Struck because the choice is the policy, and a
// policy spread across callers is a policy each caller gets to have a
// different opinion about. The three verbs stay exported for the
// caller that has already made the judgment — a wall raised from a
// response header, say — and for the tests that build a streak
// directly.
func (p *Pacer) Note(host string, o Outcome, cause error) {
	switch o {
	case Answered, NotModified:
		p.Cleared(host)
	case Refused:
		p.Wall(host, cause)
	case Unanswered:
		p.Struck(host, cause)
	case Ours:
		// Dockhand's clock, not the host's.
	}
}
