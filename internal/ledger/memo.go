package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/plan"
)

// PlanNotesRef is the notes namespace holding the decline memo: what
// dockhand already refused, keyed by the whole of what it refused.
//
// It sits here and not beside VerifyNotesRef and OutcomeNotesRef in git
// because it is the only one of the three that nothing outside this
// package names. It is worth stating what makes it a third namespace
// rather than more keys on either of those, because the difference is
// its lifetime. A verification record is working state and discard
// takes it with the branch; an audit row is meant to outlive the branch
// and nothing here prunes it; a memo is a pure cache under D8, and
// deleting this ref costs nothing but time. D21 fenced one species of
// note state that cannot be re-derived. This is plainly not a second:
// every entry under this ref can be recomputed by doing the work again.
//
// Two mechanical facts follow from the keys being object names that do
// not exist in the object database, and both belong here rather than in
// somebody's memory:
//
//   - `git notes prune --ref=dockhand/plan` empties the store. That is
//     git working correctly — it drops notes on unreachable objects, and
//     every key here is unreachable by design. It is harmless, because
//     the store is a cache. The tempting "fix" of writing each Portfile
//     into the object database with hash-object -w does not even work:
//     a notes entry's NAME is not a reachability edge, so `git gc
//     --prune=now` collects the blob anyway, and all it buys is one
//     loose object per declined port.
//   - `git notes add` writes one commit on this ref per call, so a
//     selector sweep that declines a thousand ports puts a thousand
//     commits here. That is the known cost of storing through git's own
//     porcelain, and it is paid once: the second run of the same sweep
//     hits and writes nothing. Collapsing a sweep's memos into one
//     notes-ref commit needs a tree written with no base commit to graft
//     onto, which is a git-level primitive this tree does not have yet.
const PlanNotesRef = "dockhand/plan"

// MemoFormat is the memo's own format integer, and it is a component of
// every key.
//
// It exists because the environment digest cannot see dockhand itself.
// Three decline producers in the tree are decided by this build and not
// by the port — portstyle's unsupported-field and unknown-style
// refusals are a function of the style table, and housekeeping's
// "every rule this build knows already holds" says so outright — and
// dockhand records no build version anywhere (`go build` reports
// "(devel)" and nothing reads debug.ReadBuildInfo), so there is nothing
// automatic to hash. Bump this by hand whenever a rule that produces or
// suppresses a decline changes. Old entries then become unreachable
// keys rather than wrong answers, which is the cache's own failure mode
// and costs a re-derivation.
const MemoFormat = 1

// ErrNotMemoizable reports a decline the memo refuses to keep. It is
// not a failure: re-deriving is what the caller was going to do anyway.
var ErrNotMemoizable = errors.New("ledger: this decline is not memoizable")

// ErrEnvIncomplete reports an environment digest asked for over a value
// that does not name the whole environment.
var ErrEnvIncomplete = errors.New("ledger: the environment digest needs every component")

// Env is the evaluation environment a decline was reached in: the six
// things that can change what a Portfile evaluates to without one byte
// of the Portfile moving.
//
// Every field is required, and Digest refuses a value with a blank one.
// A digest over a half-filled Env is the failure this store cannot
// survive: it would collide two environments under one key and hand a
// PortGroup-decided refusal to a machine whose PortGroups say otherwise
// — silently, and forever, because a content-addressed store does not
// expire.
type Env struct {
	// PortGroupDir is the tree's _resources/port1.0/group directory, and
	// the whole directory is digested — names and content — rather than
	// the groups this port happens to include. PortGroups include each
	// other (golang pulls github), so a per-port include set is not
	// closed and a digest over one is a digest over part of the answer.
	//
	// It is digested from the filesystem and not from a git tree id
	// because the ports tree most installations have is rsync-delivered
	// and is not a checkout at all.
	PortGroupDir string
	// MacPorts is base's own version — the port1.0 Tcl the evaluation
	// actually ran in.
	MacPorts string
	// Prefix is the installation root. Two installations on one machine
	// can carry the same base version over different macports.conf,
	// sources.conf and port1.0 libraries, and the version string does not
	// tell them apart.
	Prefix string
	// Platform is the evaluation's platform frame, already resolved to
	// the host's real os/major/arch when the caller asked for the default
	// — the zero frame renders the same on every machine, so leaving it
	// unresolved would collide two hosts.
	Platform string
	// Shim is dockhand's own Tcl, which runs inside the evaluation and
	// is therefore part of what produced the answer.
	Shim string
}

// Digest renders the environment as one hex string.
//
// The PortGroup directory is walked in lexical order and every regular
// file's path and content go into the hash, so a group edited, added or
// removed moves the digest. That walk is the only I/O here, it is a
// hundred-odd files on a real tree, and it is meant to be paid once per
// run and carried into every key.
func (e Env) Digest() (string, error) {
	switch {
	case e.PortGroupDir == "":
		return "", fmt.Errorf("%w: no PortGroup directory", ErrEnvIncomplete)
	case e.MacPorts == "":
		return "", fmt.Errorf("%w: no MacPorts version", ErrEnvIncomplete)
	case e.Prefix == "":
		return "", fmt.Errorf("%w: no prefix", ErrEnvIncomplete)
	case e.Platform == "":
		return "", fmt.Errorf("%w: no platform frame", ErrEnvIncomplete)
	case e.Shim == "":
		return "", fmt.Errorf("%w: no shim", ErrEnvIncomplete)
	}
	h := sha256.New()
	field(h, []byte(e.MacPorts))
	field(h, []byte(e.Prefix))
	field(h, []byte(e.Platform))
	field(h, []byte(e.Shim))
	if err := digestTree(h, e.PortGroupDir); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// digestTree folds a directory's regular files into h, each as its
// tree-relative path and its bytes.
//
// A directory that is not there digests as no files rather than as an
// error: a tree carrying no PortGroups is a real, if unusual, tree, and
// the day it gains one the digest moves, which is the whole contract.
// Anything else the walk hits propagates — an unreadable group would
// otherwise digest as though it were absent, and that is a collision.
func digestTree(h hash.Hash, root string) error {
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		field(h, []byte(filepath.ToSlash(rel)))
		field(h, body)
		return nil
	})
}

// MemoKey is the whole of what a memoized decline is an answer to.
//
// Every field is a component of the key, so any difference is a miss
// rather than a wrong answer. That is the store's entire safety model:
// there is no revalidation and no freshness, because a stale memo is
// not a stale answer, it is a different key. A Portfile edited to fix
// the very thing that was declined re-arms the moment it is saved.
type MemoKey struct {
	// Env is Env.Digest, computed once per run and carried into every
	// key.
	Env string
	// Intent is the verb: "bump", "refresh-checksums", "bump-revision".
	Intent string
	// Params is every run parameter that can change what the planner
	// answers, in one stable rendering. It is a string and not a struct
	// because this package must not learn the intents' vocabulary; the
	// caller renders it, and engine.MemoParams is the one renderer, so
	// there is one place where "which flags matter" is decided.
	Params string
	// Portdir is the port's directory, repository-relative. Relative and
	// not absolute so that a checkout moved on disk keeps its memos, and
	// present at all because two byte-identical Portfiles in different
	// directories are not obliged to decline alike.
	Portdir string
	// Subport is the evaluation context inside the Portfile, empty for
	// the top-level port.
	Subport string
	// Variants is the variant frame, rendered.
	Variants string
	// Portfile is the file's bytes as the working tree holds them —
	// never as HEAD holds them. Hashing what is on disk is what makes
	// the memo correct on a dirty checkout, and what makes an unsaved
	// fix a miss and a saved one a new key.
	Portfile []byte
}

// name renders the key as a hex object name of the given width.
//
// Width is the repository's, because a note's key must parse as an
// object name in the repository's own hash format: a 40-hex key in a
// sha256 repository is git's fatal band, not a miss. The digest is
// sha256 truncated to fit, which is a cache key and not a signature —
// a collision costs one wrong replay of a decline, and at 160 bits it
// will not happen.
//
// Every component is length-prefixed, so no component can spell part of
// the next one and two different keys cannot hash alike by joining
// differently.
func (k MemoKey) name(format, width int) string {
	h := sha256.New()
	field(h, []byte(strconv.Itoa(format)))
	field(h, []byte(k.Env))
	field(h, []byte(k.Intent))
	field(h, []byte(k.Params))
	field(h, []byte(k.Portdir))
	field(h, []byte(k.Subport))
	field(h, []byte(k.Variants))
	field(h, k.Portfile)
	return hex.EncodeToString(h.Sum(nil))[:width]
}

// field writes one length-prefixed component.
func field(h hash.Hash, b []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(b)))
	h.Write(n[:]) //nolint:errcheck // hash writes cannot fail
	h.Write(b)    //nolint:errcheck
}

// Memo is one repository's decline memo.
//
// It caches JUDGMENTS and is addressed by CONTENT. The observation
// cache the upstream witnesses use is its opposite in every respect and
// the two must never borrow each other's key. A memo answers: given
// exactly these bytes, in exactly this environment, dockhand already
// refused this and would refuse it again for the same reason — so its
// key is the whole of its input and it has no TTL, no revalidation and
// no notion of freshness. A witness cache answers what a forge said
// about some tags a few minutes ago; upstream is a moving target no key
// can pin, so it is bounded by time, and being allowed to go stale is
// its whole design.
//
// A judgment given a TTL would be re-derived for nothing while its
// inputs sat still, and — worse — would keep answering for a Portfile
// that changed inside the window. An observation given a content key
// would be immortal: "ls-remote said 1.2.3" would still be the answer a
// year after 1.3.0 shipped, because nothing in the port's bytes changed
// to invalidate it.
//
// They meet in exactly one place, which is ordering, and the rule is
// about the KEY. The memo may be consulted the moment every component
// of its key is settled and not one step before — which for bump means
// after upstream resolution, since the resolved input is a component,
// and for the two verbs with no resolution step means before the
// planner starts. It is NOT a rule about reaching the network: two of
// the three verbs fetch inside Plan, so a consult that waited for the
// network would be a consult after the work it was meant to save. What
// makes answering before a fetch safe is the other gate — only a
// Portfile-determined decline is ever stored. A sweep therefore pays
// the witness cache's paced, revalidating cost on every port, and the
// planner's cost only on the ports the memo misses.
//
// Nothing calls any of this yet. The composition root computes no
// environment digest, so no production run has a key; what is written
// here is the contract a caller must meet.
type Memo struct {
	repo  *git.Repo
	width int // the repository's object-name width; 0 until asked
}

// OpenMemo binds a memo to a repository. Nothing here can fail: the
// notes ref need not exist, and a repository with no memos at all reads
// as one where every key misses.
func OpenMemo(repo *git.Repo) *Memo { return &Memo{repo: repo} }

// Lookup returns the decline this key was already answered with, and
// whether there was one.
//
// A note that cannot be read as a memo is a MISS, not an error. That is
// the one place this package's usual discipline — absence is storage's
// answer, refusal is the codec's — does not apply, and the reason is
// that nothing can be lost by re-deriving: the worst a corrupt or
// future-format note costs is the work it would have saved. Storage's
// own failures still propagate, because a locked ref or an unreadable
// object is not a statement about this key.
func (m *Memo) Lookup(ctx context.Context, k MemoKey) (*plan.Decline, bool, error) {
	name, err := m.name(ctx, k)
	if err != nil {
		return nil, false, err
	}
	body, err := m.repo.NoteRead(ctx, PlanNotesRef, name)
	if err != nil {
		if errors.Is(err, git.ErrNoNote) {
			return nil, false, nil
		}
		return nil, false, err
	}
	d, ok := decodeMemo(body)
	return d, ok, nil
}

// Store records a decline as this key's answer, replacing whatever
// stood there.
//
// It refuses a decline the taxonomy does not allow it to keep, with
// ErrNotMemoizable, and that refusal is the store's own gate rather
// than a rule the callers share: there is one road into this ref and it
// asks plan.Decline.Memoizable itself, so no call site can decide to
// remember a network-decided refusal.
//
// No lock. The notes lock exists for read-modify-write of one document,
// and this is neither: a lost write between two dockhands writing
// different keys costs a re-derivation, which is what the memo was
// saving in the first place.
func (m *Memo) Store(ctx context.Context, k MemoKey, d *plan.Decline) error {
	if d == nil || !d.Memoizable() {
		return fmt.Errorf("%w: %s", ErrNotMemoizable, declineWord(d))
	}
	name, err := m.name(ctx, k)
	if err != nil {
		return err
	}
	body, err := encodeMemo(d)
	if err != nil {
		return err
	}
	return m.repo.NoteWrite(ctx, PlanNotesRef, name, body)
}

// Forget drops one key's memo. A key with no memo is fine — removal is
// idempotent.
func (m *Memo) Forget(ctx context.Context, k MemoKey) error {
	name, err := m.name(ctx, k)
	if err != nil {
		return err
	}
	return m.repo.NoteRemove(ctx, PlanNotesRef, name)
}

// name resolves the key against this repository's object-name width,
// asking git once.
//
// The width is read off HEAD rather than from `rev-parse
// --show-object-format` because the width is the whole of what matters
// here and HEAD's own name is that width. A repository with no commit
// at all has nothing to annotate either, and says so loudly instead of
// writing keys git will later refuse to resolve.
func (m *Memo) name(ctx context.Context, k MemoKey) (string, error) {
	if m.width == 0 {
		head, err := m.repo.RevParse(ctx, "HEAD")
		if err != nil {
			return "", fmt.Errorf("ledger: memo needs the repository's object format: %w", err)
		}
		if !isHex(head) {
			return "", fmt.Errorf("ledger: memo cannot size a key: HEAD resolved to %q", head)
		}
		m.width = len(head)
	}
	return k.name(MemoFormat, m.width), nil
}

// isHex reports a plausible object name: hex, and of a width sha256's
// hex can be cut down to.
func isHex(s string) bool {
	if len(s) == 0 || len(s) > 2*sha256.Size {
		return false
	}
	return strings.TrimLeft(s, "0123456789abcdef") == ""
}

// memoNote is what one memo holds on the wire: the decline's stable
// code and the sentence its producer wrote.
//
// Withheld is absent because a decline carrying it is never stored —
// see plan.Decline.Memoizable. The determinacy is absent because a memo
// that exists is Portfile-determined by this store's own rule, so a hit
// says so rather than repeating it into the note.
type memoNote struct {
	Format int    `json:"format"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// encodeMemo renders a decline for the note.
func encodeMemo(d *plan.Decline) ([]byte, error) {
	return json.Marshal(memoNote{Format: MemoFormat, Code: d.Type.Code(), Detail: d.Detail})
}

// decodeMemo reads a note back, answering "not a memo I can use" rather
// than an error — every refusal here is a miss, and a miss re-derives.
//
// The format is checked even though it is already a key component. The
// key makes an old note unreachable and this makes an unreadable one
// harmless, and the two failures are different enough to be worth
// guarding separately.
func decodeMemo(body []byte) (*plan.Decline, bool) {
	var n memoNote
	if err := json.Unmarshal(body, &n); err != nil || n.Format != MemoFormat {
		return nil, false
	}
	t, ok := plan.DeclineTypeFor(n.Code)
	if !ok {
		return nil, false
	}
	return &plan.Decline{Type: t, Detail: n.Detail, Determined: plan.ByPortfile}, true
}

// declineWord says why the store would not keep this one, so the
// refusal names the rule rather than only the decline.
func declineWord(d *plan.Decline) string {
	switch {
	case d == nil:
		return "there is no decline to keep"
	case len(d.Withheld) > 0:
		return d.Type.Code() + " withheld riders, which the key does not name"
	case d.Determined == plan.ByNetwork:
		return d.Type.Code() + ": its producer says network-determined"
	default:
		return d.Type.Code() + " is " + d.Type.Determinacy().String()
	}
}
