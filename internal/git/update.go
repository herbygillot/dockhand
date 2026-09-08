package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

// RefUpdate is one line of an update-ref batch, and it is the whole
// vocabulary of ref movement this design has. Three forms, told apart by
// which field is empty, so a caller cannot ask for "move it, whatever it
// holds" — the form the shipped DeleteBranch had (`branch -D`, no
// expected value), and the form that lets a discard delete work it never
// resolved:
//
//	Old ""  New sha  create: the ref must not exist
//	Old sha New sha  update: the ref must hold Old (New == Old is an
//	                 ASSERTION — no move, the lock still taken, the
//	                 expected value still checked, measured on git 2.55;
//	                 change.AdoptIn and change.FollowIn use it)
//	Old sha New ""   delete: the ref must hold Old
//
// Both empty is refused (ErrBadUpdate): a line that means nothing is a
// caller that forgot which sha it was asserting, and rule 7 says an
// empty expected value must never read as "no check".
type RefUpdate struct {
	Ref string // fully qualified: refs/dockhand/state, refs/heads/dockhand/<name>, refs/dockhand/verify/<id>
	New string // "" = delete
	Old string // "" = must not exist
}

// ErrRefMoved is the sentinel every refused batch is classified under: a
// ref in the batch is not at the value the caller expected. The typed
// form, RefMoved, names which. Callers branch on this with errors.Is and
// read the ref with errors.As — never on the text git printed, because
// git reports the failing ref only in stderr prose ("cannot lock ref
// 'refs/dockhand/state': is at one but expected two"), and rule 6 says
// nothing recovers a fact by reading words.
var ErrRefMoved = errors.New("git: a ref in the batch is not at the value the caller expected")

// RefMoved is ErrRefMoved with the ref named, found by RE-READING the
// batch after git refused it, never by parsing the refusal. Expected ""
// means the line asked for absence (a create) and Found is what stands
// there; Found "" means the ref is gone and the line expected Expected.
// A re-read that FAILED is returned as its own error and never as this
// value (rule 7). It unwraps to ErrRefMoved so one sentinel covers every
// road and one errors.As recovers the ref for the message.
type RefMoved struct {
	Ref      string
	Expected string
	Found    string
}

func (e *RefMoved) Error() string {
	return fmt.Sprintf("git: %s is at %q, expected %q", e.Ref, e.Found, e.Expected)
}
func (e *RefMoved) Unwrap() error { return ErrRefMoved }

// ErrBadUpdate is UpdateRefs refusing a batch it cannot mean: an empty
// batch, a line with neither New nor Old, or a ref named twice (git
// refuses the last two itself, with words — "missing <new-oid>",
// "multiple updates for ref … not allowed" — and this package refuses
// them typed, before git). statestore.Txn.Ref returns the same sentinel
// for a duplicate name it cannot coalesce, so one sentinel names one
// kind of caller's slip.
//
// The both-empty line is the one that has to be refused HERE rather than
// left to git, and the measurement says why: git does not refuse it. On
// git 2.55.0 `update <ref> NUL NUL` warns "missing <new-oid>, treating
// as zero", exits 0, and DELETES the ref without checking anything. A
// caller that meant "assert this ref" and lost its sha would silently
// destroy the ref it was asserting.
var ErrBadUpdate = errors.New("git: a ref line names nothing to do, or a ref is named twice")

// reflogMessage is the -m every batch carries: what a person sees in the
// `ref` column's message when they read `git reflog` over a ref dockhand
// moved. It is for the person and never for a decision — nothing reads
// it back, because reading a fact out of a message is rule 6's whole
// prohibition.
const reflogMessage = "dockhand"

// UpdateRefs performs one `git update-ref -z --stdin` batch, all or
// nothing, and it is the ONLY function in the design that moves a ref.
// check_callers.py holds it to exactly one caller, statestore.Amend, so
// that "the store's commit is the only mover of the refs that are
// authority" is a fact the harness re-proves on every build rather than
// a sentence in a doc.
//
// Atomicity is git's, not dockhand's: with --stdin every line is locked
// before any is written, and one refused line rolls the rest back. Proven
// on this machine (git 2.55.0) in three cases — a wrong expected-old on
// the state line leaves the branch uncreated; a wrong expected-old on a
// branch delete leaves the state line unmoved too. That is what lets a
// record and the ref it names land in one act, which is the ruling.
//
// ON A REFUSAL it classifies by re-read (rule 6): every ref in the batch
// is read back with RevParse, IN BATCH ORDER — the state ref is always
// the first line, so a lost race on it is named before any foreign move
// behind it — and the first whose value is not its line's Old is
// returned as *RefMoved. Nothing moved, so the re-read is against the
// state git refused over. If every ref reads as expected the refusal
// was git's own failure — a locked packed-refs, a full disk, a .lock a
// person's porcelain holds — and it is handed on as it came, untyped,
// because it IS untyped, and not retried: a retry that cannot say what
// it retries is check-then-act. It refuses ErrBadUpdate before touching
// git; it writes with -z so a ref name cannot smuggle a newline into the
// batch; and it passes -m so a person reading `git reflog` sees
// dockhand's name, which is for the person and never for a decision.
//
// The re-read is rev-parse under its exit code and not under its output,
// because absence and failure both come back as no sha: exit 1 is "the
// ref is not there", and every other non-zero is "the repository could
// not be asked", which leaves as itself. Rule 7 again — a re-read that
// failed must never be reported as a ref that moved.
//
// THE WIRE IS THIS VERB'S, STATED ONCE. In -z mode an EMPTY <oldvalue>
// FIELD means "unverified" — git's word for it is "missing value" — and
// NOT "must not exist": measured, `update <ref> NUL <new> NUL <empty>`
// moved a standing ref with rc 0 and `delete <ref> NUL <empty>` deleted
// unchecked, where the `create` command and the zero oid were both
// refused ("reference already exists"). So RefUpdate's field convention
// is translated here and nowhere else: Old "" is emitted as the `create`
// command; New "" as `delete <ref> NUL <old>` with old always present;
// everything else as `update <ref> NUL <new> NUL <old>`. An empty value
// FIELD is never written to git, and no caller can reach one, because
// ErrBadUpdate refuses the both-empty line first. Build step 3's done
// criterion carries the measured test: a create line against a standing
// ref is refused.
//
// Every line is preceded by `option no-deref` (an option applies to the
// next command only). A symref a hand planted under refs/heads/dockhand/
// would otherwise carry a dockhand move or delete onto its TARGET —
// measured: a batch line for such a symref moved refs/heads/master and a
// delete line deleted it, the symref surviving both — while under
// no-deref the symref itself is overwritten as a plain ref or deleted,
// which is dockhand's own name to overwrite. statestore.Txn.Ref judges
// NAMES and cannot see through a symref; this option is what makes "only
// mover" true of the target as well as the name.
//
// The reflog covers every create and update in a batch under
// statestore.LogRef. It does NOT cover a delete: git removes a ref's
// reflog with the ref (measured), so the recovery path for a demolished
// branch is the state ref's own history, where the closed record names
// the Tip — bounded by statestore.PruneExpire, stated there once. It
// refuses NOTHING about worktrees: a delete line removes a branch some
// worktree has checked out (measured), and the guard `branch -D` owned
// is an OBSERVATION app makes before the Amend (CheckedOutAt), not this
// verb's decision (rule 1) — and an update line MOVES a branch some
// worktree has checked out (measured; `branch -f` refuses), so the same
// observation guards Accept's extend road.
func (r *Repo) UpdateRefs(ctx context.Context, updates []RefUpdate) error {
	batch, err := encodeUpdates(updates)
	if err != nil {
		return err
	}
	if _, err := r.gitStdin(ctx, batch, "update-ref", "-z", "-m", reflogMessage, "--stdin"); err != nil {
		if moved, rerr := r.classifyRefusal(ctx, updates); rerr != nil {
			return rerr
		} else if moved != nil {
			return moved
		}
		return err
	}
	return nil
}

// encodeUpdates renders the batch git reads on stdin, refusing every
// line this package will not put on the wire first. The refusals come
// before a single byte is written because a batch is all-or-nothing only
// once git has it: a bad line discovered halfway through the encoding
// would still be a batch nobody sent, but a bad line git accepts — the
// both-empty update it treats as an unchecked delete — is a ref already
// gone.
func encodeUpdates(updates []RefUpdate) ([]byte, error) {
	if len(updates) == 0 {
		return nil, fmt.Errorf("%w: an empty batch", ErrBadUpdate)
	}
	named := make(map[string]bool, len(updates))
	var batch bytes.Buffer
	for _, u := range updates {
		switch {
		case u.Ref == "":
			return nil, fmt.Errorf("%w: a line with no ref", ErrBadUpdate)
		case strings.ContainsRune(u.Ref, 0):
			// -z makes NUL the field separator, so a name carrying one
			// would not be one line but two, and the second would be
			// whatever the first half's remainder happened to spell.
			return nil, fmt.Errorf("%w: %q is not a ref name", ErrBadUpdate, u.Ref)
		case u.New == "" && u.Old == "":
			return nil, fmt.Errorf("%w: %s names neither a new value nor an expected one", ErrBadUpdate, u.Ref)
		case named[u.Ref]:
			return nil, fmt.Errorf("%w: %s", ErrBadUpdate, u.Ref)
		}
		named[u.Ref] = true
		batch.WriteString("option no-deref\x00")
		switch {
		case u.Old == "":
			batch.WriteString("create " + u.Ref + "\x00" + u.New + "\x00")
		case u.New == "":
			batch.WriteString("delete " + u.Ref + "\x00" + u.Old + "\x00")
		default:
			batch.WriteString("update " + u.Ref + "\x00" + u.New + "\x00" + u.Old + "\x00")
		}
	}
	return batch.Bytes(), nil
}

// classifyRefusal is the re-read UpdateRefs turns a refused batch into a
// fact with. It returns the first line whose ref is not where that line
// expected it, nil when every ref stands as expected — which says the
// refusal was git's own and belongs to the caller as it came — and a
// second error when the re-read itself could not be made, which is
// neither of those and must never be reported as either.
func (r *Repo) classifyRefusal(ctx context.Context, updates []RefUpdate) (*RefMoved, error) {
	for _, u := range updates {
		now, err := r.refNow(ctx, u.Ref)
		if err != nil {
			return nil, fmt.Errorf("git: re-reading %s after a refused batch: %w", u.Ref, err)
		}
		if now != u.Old {
			return &RefMoved{Ref: u.Ref, Expected: u.Old, Found: now}, nil
		}
	}
	return nil, nil
}

// refNow is what a ref holds at this instant: its object name, "" when
// no such ref exists, and an error when the repository could not be
// asked at all. The three are told apart by exit code and never by
// output, because absence and failure both print nothing: `rev-parse
// --verify --quiet` exits 1 for a ref that is not there (and for one
// whose file git calls broken, which is a ref that is not usable either)
// and 128 for the fatal band — no repository, an object store it cannot
// read. Rule 7 is the whole reason this is not RevParse: RevParse hands
// back "" with a non-nil error for both, and a classifier that read that
// zero as "the ref is gone" would report a moved ref every time git
// failed to answer.
func (r *Repo) refNow(ctx context.Context, ref string) (string, error) {
	out, code, err := execGit(ctx, r.tools, r.Root, nil, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		if code == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// CheckedOutAt reports the worktree in which branch is HEAD, or "" with a
// nil error when no worktree has it checked out, read from `git worktree
// list --porcelain`'s `branch refs/heads/<name>` lines. It exists because
// the shipped DeleteBranch was `git branch -D`, the one porcelain call in
// the package, and porcelain owned a refusal plumbing does not: `branch
// -D` will not delete a branch some worktree has checked out, where a
// `delete` line in an update-ref batch will, and leaves that worktree's
// HEAD pointing at nothing — and an `update` line MOVES such a branch
// where `branch -f` refuses, leaving the worktree's HEAD past its index
// so the person's next commit re-commits the old content over the new
// (measured). The guard moves to where rule 3 puts it — an OBSERVATION
// every road whose batch would move or delete a branch makes before its
// Amend: Accept's extend, and the replace, supersede, discard and retire
// roads — and the refusal is change.ErrCheckedOut (exit 46). A non-nil
// error is "could not read the worktree list", never "not checked out"
// and never "checked out" (rule 7): a verb road returns it as itself
// (band 1, its own remedy), a pass road keeps the branch and carries the
// error in its Refusal row. The second-wide window between this read and
// the batch is the window every observation-then-Amend in the design
// accepts, and D41 states it.
func (r *Repo) CheckedOutAt(ctx context.Context, branch string) (string, error) {
	out, err := r.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	// The porcelain form is a stanza per worktree — `worktree <path>`,
	// `HEAD <sha>`, then `branch <fully qualified ref>` for a worktree
	// that is on a branch and nothing for a detached one — separated by
	// blank lines. The branch line is matched whole against the fully
	// qualified name: a prefix match would read dockhand/jq-1.8's
	// worktree as dockhand/jq's.
	want := "branch refs/heads/" + branch
	at := ""
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\n")
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			at = path
			continue
		}
		if line == want {
			return at, nil
		}
	}
	return "", nil
}
