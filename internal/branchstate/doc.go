// Package branchstate is the one verdict on a branch's state, from what's
// recorded of it: whether its checks stand for its files, and, as part 4
// of v0.5.0 brings each judgment here, what it's ready for, what holds it,
// and what moves it on, which status, serve, submit, tidy, and ship each
// judged apart (the direction synthesis's layer 1). It's pure: it reads
// the facts the engine gathers and returns a verdict, reading no store,
// asking no provider, and wording nothing, which its callers do.
package branchstate
