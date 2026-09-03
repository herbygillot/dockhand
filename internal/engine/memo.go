package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/plan"
)

// Memoized runs a planner under the decline memo: consult it first, and
// on a miss plan, keeping the decline when the taxonomy allows it.
//
// NOTHING CALLS THIS YET. The key, the digest, the taxonomy gate and
// the store are built and tested; the composition root does not compute
// an environment digest, so no production run has a key. Read what
// follows as the contract a caller must meet, not as a description of
// what the tool does today.
//
// WHERE THIS IS CALLED IS THE ORDERING RULING, and the ruling is about
// the KEY and not about the network. The memo may be consulted the
// moment every component of its key is settled, and not one step
// before. For bump that means after upstream resolution, because the
// resolved version is a key component: a run that has not yet asked
// upstream what "latest" means does not know what it is asking the memo
// about. For refresh-checksums and bump-revision there is no resolution
// step at all, and the key is complete before the planner starts.
//
// It was once written here as "after upstream resolution, never
// before", which reads as a rule about reaching the network and is not
// one. Two of the three verbs reach the network INSIDE Plan — refresh
// fetches distfiles with the mirrors deliberately disabled — so a
// consult that waited for the network would be a consult after the
// work it was meant to save. What makes it safe to answer before a
// fetch is the other gate entirely: only a Portfile-determined decline
// is ever stored, and a decline that a fetch decided says so through
// plan.Determinacy and is refused. The two rules do different jobs, and
// conflating them either strands the memo or lets it suppress a fetch.
//
// LatestUnresolved is structurally unreachable here, quite apart from
// the taxonomy refusing it: that decline IS the resolution failing, so
// there is no resolved input to key on.
//
// Nothing about a hit is different from a fresh run except the work not
// done. The same *plan.Decline comes back, so the same sentence, the
// same code, the same exit band. A single-target invocation that
// declines is byte-identical whether it was remembered or re-derived.
//
// The memo is a cache and behaves like one at every failure. A tree
// with no repository behind it has no memo, a git error consulting one
// is a miss, and a git error keeping one is dropped: none of them
// changes what the run answers, only what it had to spend to answer.
// Each is logged at debug, because a memo that never hits is a
// performance bug and silence would hide it.
func (e *Engine) Memoized(ctx context.Context, k ledger.MemoKey, portdir string, run func(context.Context) (*plan.Plan, error)) (*plan.Plan, error) {
	memo, key, ok := e.memoFor(ctx, k, portdir)
	if ok {
		if d, hit, err := memo.Lookup(ctx, key); err != nil {
			slog.Debug("decline memo unreadable", "portdir", portdir, "err", err)
		} else if hit {
			slog.Debug("decline memo hit", "portdir", portdir, "code", d.Type.Code())
			return nil, d
		}
	}
	p, err := run(ctx)
	if err == nil || !ok {
		return p, err
	}
	// A type assertion and not errors.As, deliberately. A decline that
	// arrives wrapped had something added to it by the wrapper — an exit
	// band, a resolution's own verdict — and a hit replays the decline
	// alone, so remembering a wrapped one would answer a later run with
	// less than the first run said.
	d, bare := err.(*plan.Decline) //nolint:errorlint // the wrapping is the thing being tested for
	if !bare || !d.Memoizable() {
		return p, err
	}
	if serr := memo.Store(ctx, key, d); serr != nil {
		slog.Debug("decline memo not kept", "portdir", portdir, "err", serr)
	}
	return p, err
}

// memoFor opens the memo for the repository the portdir lives in and
// completes the key with the portdir as that repository names it.
//
// Repository-relative, because the memos travel with the checkout: a
// clone moved on disk, or a second worktree of the same repository,
// must not read as a different tree. A portdir outside any repository —
// or a tree that is not a checkout at all, which is what an
// rsync-delivered ports tree is — simply has no memo, and the run pays
// full price.
func (e *Engine) memoFor(ctx context.Context, k ledger.MemoKey, portdir string) (*ledger.Memo, ledger.MemoKey, bool) {
	if k.Env == "" {
		// No environment digest is not a degraded key, it is a missing
		// one: every answer here depends on the PortGroups and the base
		// that produced it, and a memo keyed without them would hand one
		// installation's refusal to another.
		slog.Debug("decline memo skipped: no environment digest", "portdir", portdir)
		return nil, k, false
	}
	repo, err := e.RepoFor(ctx, portdir)
	if err != nil {
		slog.Debug("decline memo skipped: no repository", "portdir", portdir, "err", err)
		return nil, k, false
	}
	rel, err := repo.RelPath(portdir)
	if err != nil {
		slog.Debug("decline memo skipped: portdir outside the repository", "portdir", portdir, "err", err)
		return nil, k, false
	}
	k.Portdir = rel
	return ledger.OpenMemo(repo), k, true
}

// MemoParams renders every run parameter that can change what a planner
// answers, as the key's Params component.
//
// It is one function because "which flags matter" must be decided in
// one place. The hazard it exists to close is concrete: bump's
// --recheck reaches an UnexpectedChange — "version moved from X during
// a re-derivation at the same version" — that no plain bump can reach,
// and that decline's type is memoizable. Keyed without the flag, a
// re-derivation's refusal would be replayed at an ordinary bump of the
// same port, which is a wrong answer rather than a missing one.
//
// Named fields with their values, sorted, so that a field added to
// Params and forgotten here shows up as a missing name rather than as
// two parameter sets hashing alike. What is deliberately absent:
//
//   - Target, because the portdir and subport are key components
//     already, and the same port typed two ways is the same port.
//   - Latest, because a resolved version is a resolved version however
//     it was arrived at, and Version below carries the answer.
//   - Tools and Dependents, which are the run's discoveries rather than
//     the user's parameters. Dependents feeds the instruction-comment
//     finding rule, and a finding rides on a plan, never on a decline.
//   - Cohort, which is empty at every call site the catalogue has.
func MemoParams(p intent.Params) string {
	fields := []string{
		"version=" + p.Version,
		"reason=" + p.Reason,
		"closes=" + p.ClosesTicket,
		"recheck=" + strconv.FormatBool(p.Recheck),
		"riders=" + riderWord(p.Riders),
	}
	sort.Strings(fields)
	return strings.Join(fields, "\x00")
}

// riderWord names a rider policy in the key.
//
// A word and not the enum's number: the numbers are declaration order,
// and reordering the constants would change what every stored key meant
// without changing a single one of them. An unnamed policy renders as
// itself, which is a miss rather than a collision with a named one.
func riderWord(p intent.RiderPolicy) string {
	switch p {
	case intent.RidersAlong:
		return "along"
	case intent.RidersOnly:
		return "only"
	case intent.RidersNone:
		return "none"
	}
	return "policy-" + strconv.Itoa(int(p))
}

// MemoEnv is the environment digest for one run, computed once.
//
// The components arrive as values rather than being discovered here on
// purpose: what the running MacPorts is, where its prefix sits and what
// Tcl dockhand injected are the composition root's facts, and an engine
// that went looking for them would answer differently in a test than in
// a run. The digest refuses a component left blank, so a caller that
// forgets one gets an error and no memo, never a memo keyed on a gap.
func MemoEnv(env ledger.Env) (string, error) {
	digest, err := env.Digest()
	if err != nil {
		return "", fmt.Errorf("memo: %w", err)
	}
	return digest, nil
}
