// Package refusal is why something dockhand was asked to do didn't go, as a
// kind, its words, what was kept as it was, and what moves it on: the one
// shape every layer refuses in, where ten sentences with their remedies
// were composed below the engine (the architecture review of 5897a82,
// R3-31), commands read an exit error as a verdict and cut words out of
// errors (R3-38), and a failure another try may meet had five shapes
// (R3-6). It imports nothing of dockhand's, so the packages that refuse,
// fetch, the providers, GitHub's login, the store, and the coordination,
// can each fill it.
package refusal

import (
	"errors"
	"fmt"
)

// Kind is what kind of refusal it is, which decides its exit code and
// whether another try may meet it. Each kind's doc says who gives its
// Next: the producer, where the remedy is unconditional and its own, or
// the engine and branchstate, where it depends on the branch's state.
type Kind string

const (
	// Network is a request that didn't reach its service, or a service
	// that failed it: another try may meet it. Its producer gives no
	// Next; trying again is the remedy.
	Network Kind = "network"
	// Infrastructure is a build environment that failed, a VM or a runner,
	// rather than what it built: another try, in a fresh one, may meet it.
	// No Next; the retry rule tries again.
	Infrastructure Kind = "infrastructure"
	// Limited is a service's allowance spent, as GitHub's rate limit:
	// another try once it resets may meet it. No Next.
	Limited Kind = "limited"
	// UpstreamNotYet is an archive or a release a service doesn't serve
	// yet, as mirrors behind a new release: another try, later, may meet
	// it. No Next.
	UpstreamNotYet Kind = "upstream-not-yet"
	// UpstreamMissing is an archive or a release that isn't where the
	// Portfile says. The engine gives its Next, which depends on the
	// branch: accept, skip, or edit.
	UpstreamMissing Kind = "upstream-missing"
	// Auth is a login that's gone, expired, or refused. Its producer's
	// Next is setup github.
	Auth Kind = "auth"
	// Unsupported is an edit dockhand doesn't make, which a person makes
	// by hand. The engine gives its Next: edit.
	Unsupported Kind = "unsupported"
	// Environment is a build environment not set up, as a Tart image
	// missing. Its producer's Next is the setup that makes it.
	Environment Kind = "environment"
	// Busy is the store or a check held by another process. Its
	// producer gives a Next where one command waits it out; a migration
	// held by running processes has none (wait for them, or stop them).
	Busy Kind = "busy"
	// Attention is what waits for a person's look, which exits 3.
	// branchstate gives its Next.
	Attention Kind = "attention"
	// CheckFailed is a check that failed, which exits 2.
	CheckFailed Kind = "check-failed"
	// Interrupted is work a person or a signal stopped, which exits 130.
	Interrupted Kind = "interrupted"
	// Failed is any other refusal, which exits 1.
	Failed Kind = "failed"
)

// Transient reports whether another try may meet a refusal of the kind.
func (k Kind) Transient() bool {
	return k == Network || k == Infrastructure || k == Limited || k == UpstreamNotYet
}

// Outcome reports whether a refusal of the kind is the work's outcome, a
// check failed, waiting for a look, or stopped, rather than a failure to do
// it: the kinds that exit other than 1.
func (k Kind) Outcome() bool { return k.Exit() != 1 }

// Exit is the kind's exit code, the one table of them: a check failed 2,
// waiting for a look 3, interrupted 130, and every other refusal 1.
func (k Kind) Exit() int {
	switch k {
	case CheckFailed:
		return 2
	case Attention:
		return 3
	case Interrupted:
		return 130
	}
	return 1
}

// Refusal is why something didn't go: its kind, its words, what was kept
// as it was, where something was, and what moves it on, where its producer
// knows. It's an error, reached with errors.As through any wrapping, and
// made only by New, which requires a kind: nothing reads its words.
type Refusal struct {
	kind  Kind
	words string
	// Kept is what was left as it was, in words, by whoever knows: "the
	// branch, unchanged", "nothing was changed".
	Kept string
	// Next is what moves it on, step by step in order, each a dockhand
	// command or a remedy in words, where the refusal's producer knows;
	// empty where the engine or branchstate gives it (Kind). Most have one
	// step; a script wanting "the" command takes the last with a Verb.
	Next []Next
	// cause is what the refusal was made of, where it wraps an error.
	cause error
	// of is the refusal this one is a particular of (Because), whose
	// words lead its own when it's said.
	of *Refusal
}

// New is a refusal of a kind, in words.
func New(kind Kind, format string, args ...any) *Refusal {
	if kind == "" {
		panic("refusal: a refusal has a kind")
	}
	return &Refusal{kind: kind, words: fmt.Sprintf(format, args...)}
}

// Wrap is a refusal of a kind, in words, made of an error, which errors.Is
// and errors.As reach through it.
func Wrap(kind Kind, cause error, format string, args ...any) *Refusal {
	r := New(kind, format, args...)
	r.cause = cause
	return r
}

// Kind is the refusal's kind.
func (r *Refusal) Kind() Kind { return r.kind }

// Words are the refusal's sentence, without what was kept.
func (r *Refusal) Words() string { return r.words }

func (r *Refusal) Error() string {
	said := r.words
	if r.of != nil {
		said = r.of.words + ": " + said
	}
	if r.Kept != "" {
		return said + "\nKept: " + r.Kept + "."
	}
	return said
}

func (r *Refusal) Unwrap() error { return r.cause }

// Because is a particular of a refusal, as portfile.ErrUnsupported's
// "cargo.update can't be read as yes or no": of its kind, in its own words,
// formatted as fmt.Errorf formats them, and the refusal too, which
// errors.Is reaches, as it reaches each error a %w names. Said, r's words
// lead; its Words are its own, which whoever says why it didn't go reads
// rather than cutting r's off the error (R3-38).
func (r *Refusal) Because(format string, args ...any) *Refusal {
	particular := fmt.Errorf(format, args...)
	c := New(r.kind, "%s", particular.Error())
	c.cause, c.of = errors.Join(r, particular), r
	return c
}

// With is the refusal with what was kept as it was.
func (r *Refusal) With(kept string) *Refusal {
	c := *r
	c.Kept = kept
	return &c
}

// Then is the refusal with what moves it on, its steps in order.
func (r *Refusal) Then(steps ...Next) *Refusal {
	c := *r
	c.Next = steps
	return &c
}

// Advances reports whether what moves the refusal on moves on from the
// action it refuses, named by its verb: some step names another verb, or
// the same one once something has changed (Next.Advances).
func (r *Refusal) Advances(refused string) bool {
	for _, step := range r.Next {
		if step.Advances(refused) {
			return true
		}
	}
	return false
}

// Of is the refusal an error is, or wraps; nil where it's none.
func Of(err error) *Refusal {
	var r *Refusal
	if errors.As(err, &r) {
		return r
	}
	return nil
}

// WordsOf are why an error says something didn't go, in its own words:
// those of the first error in its chain that says them (a refusal, or a
// type with Words of its own), without the sentinel it's a particular of,
// "cargo.update can't be read as yes or no" for portfile's unsupported
// edit; else the error's. Whoever says why reads them, rather than cutting
// a sentinel's words off the error (R3-38).
func WordsOf(err error) string {
	var worded interface{ Words() string }
	if errors.As(err, &worded) {
		return worded.Words()
	}
	return err.Error()
}

// KindOf is the kind of refusal an error is, or Failed where it's none.
// Of errors joined, as upstream's and a mirror's failures are, it's the
// first that another try may meet, else the first: one part of them that
// may go another time makes the whole worth another try.
func KindOf(err error) Kind {
	found := kinds(err)
	for _, kind := range found {
		if kind.Transient() {
			return kind
		}
	}
	if len(found) > 0 {
		return found[0]
	}
	return Failed
}

// kinds are the kinds of the outermost refusals an error is or holds, on
// each of its paths: through wrapping, and each of a join's parts.
func kinds(err error) []Kind {
	switch e := err.(type) {
	case nil:
		return nil
	case *Refusal:
		return []Kind{e.kind}
	case interface{ Unwrap() []error }:
		var found []Kind
		for _, part := range e.Unwrap() {
			found = append(found, kinds(part)...)
		}
		return found
	case interface{ Unwrap() error }:
		return kinds(e.Unwrap())
	}
	return nil
}

// Transient reports whether another try may meet an error: one that is,
// or wraps, a refusal of a transient kind, or joins one, as upstream's and
// a mirror's failures are joined (KindOf). It's the one reading of it,
// which the retry rule and collecting an assessment again read (R3-6,
// where it had five shapes).
func Transient(err error) bool {
	return err != nil && KindOf(err).Transient()
}
