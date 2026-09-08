package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tool"
)

// ErrNoObject reports a request the repository could not resolve to an
// object — a ref that is not there, a sha nothing wrote. It is a
// sentinel because absence is an ANSWER on this road and not a failure:
// statestore.Read asks for the state ref's tip and turns exactly this
// into statestore.ErrNoState, where a session that broke mid-stream
// must never read as "the ref is gone" (rule 7). git's own wire says
// the same thing: a missing object is a normal `<request> missing`
// line and the process exits zero.
var ErrNoObject = errors.New("git: the repository has no such object")

// Object is one object as a batch session answered for it: the name git
// resolved the request to, what kind of thing it is, and its bytes
// verbatim. The bytes are the object's own content — a blob's file, a
// tree's binary entries, a commit's header and message — with no
// trailing newline of git's framing left on.
type Object struct {
	OID  string
	Type string
	Data []byte
}

// TreeEntry is one entry of a tree object, parsed from the raw bytes a
// batch session hands back rather than from `ls-tree`'s text. The
// binary form is parsed HERE, in the package whose job is knowing what
// git's objects look like, so that a caller reading a flat tree of
// records gets names and object ids and never a format.
type TreeEntry struct {
	// Mode is the entry's octal mode as the tree records it — "100644"
	// for a file, "40000" for a directory (trees record no leading
	// zero). A caller that only wants files tells them apart by it.
	Mode string
	Name string
	OID  string
}

// Dir reports an entry that is a subtree rather than a file.
func (e TreeEntry) Dir() bool { return strings.HasPrefix(e.Mode, "4") }

// Batch is ONE long-lived `git cat-file --batch` session: requests go
// down its stdin one line at a time and objects come back up its
// stdout, for as long as the caller keeps it open.
//
// It is the escalation this package's own doc named before anything
// needed it — "one long-lived `git cat-file --batch` session — the eval
// pattern — not a library" — and statestore.Read is what needed it.
// A read of the state ref is O(N) in the records it holds, and with
// BlobAt (one subprocess per object) it is O(N) SUBPROCESSES: measured
// on this machine, git 2.55, APFS, fifty thousand records cost 539
// seconds that way and 444 milliseconds this way. The difference
// between a store that scales and one that is unusable at a thousand
// records is this type.
//
// The session is STRICTLY ALTERNATING — one request written, one
// response read in full, then the next — which is what makes it safe
// without a goroutine: git writes a response only after reading a
// request, and the response is drained before the next request goes
// down, so neither pipe can fill while the other side is blocked on it.
// A caller that wanted concurrency would have to bring its own session;
// a *Batch is not safe for concurrent use, and there is no lock here
// because a second user of one session is a bug rather than a race to
// arbitrate.
//
// A session that has failed stays failed: the first transport error is
// remembered and every later request returns it, because a half-read
// response leaves the stream at an offset nothing can recover from and
// a "recovered" read after one would return another object's bytes.
type Batch struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	errs *bytes.Buffer
	// broken is the first transport failure, kept so that every request
	// after it answers with it rather than with whatever bytes happen to
	// be next in the stream.
	broken error
	// reaped records that the process has already been waited for, so
	// that Close after a failure does not wait a second time — the
	// second Wait is an error about this package rather than about the
	// repository, and it would replace the diagnosis git gave.
	reaped bool
}

// CatFile starts a batch session against the repository. The caller
// must Close it; a session left open holds a git process for the life
// of the process that forgot it.
func (r *Repo) CatFile(ctx context.Context) (*Batch, error) {
	bin, err := r.tools.Find(tool.Git)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "-C", r.Root, "cat-file", "--batch")
	cmd.Env = append(scrubbedEnv(), "GIT_PAGER=cat")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	// stderr is collected rather than inherited: a session that dies
	// says why on it, and that sentence is the only diagnosis a caller
	// gets for a repository git cannot open at all.
	errs := &bytes.Buffer{}
	cmd.Stderr = errs
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	return &Batch{cmd: cmd, in: in, out: bufio.NewReader(out), errs: errs}, nil
}

// Object reads one object. name is anything git's revision grammar
// accepts — a sha, a ref, `<ref>^{commit}`, `<commit>^{tree}` — and
// ErrNoObject is the answer for one the repository cannot resolve.
//
// Do NOT address a record as `<tree>:<name>`. git re-inflates the whole
// tree object on every path lookup, which makes a walk of N records
// quadratic in N; read the tree once with Tree and ask for the object
// ids it gives.
func (b *Batch) Object(name string) (Object, error) {
	if b.broken != nil {
		return Object{}, b.broken
	}
	// A request is one LINE, so a name carrying a newline would be two
	// requests and the second would be answered into the first's
	// response slot — every object after it off by one.
	if strings.ContainsAny(name, "\n\x00") {
		return Object{}, fmt.Errorf("git cat-file: %q is not a request", name)
	}
	if _, err := io.WriteString(b.in, name+"\n"); err != nil {
		return Object{}, b.fail(err)
	}
	header, err := b.out.ReadString('\n')
	if err != nil {
		return Object{}, b.fail(err)
	}
	fields := strings.Fields(header)
	// `<request> missing` is the whole of git's absence wire, and
	// `<request> ambiguous` is the whole of its "I will not guess"; both
	// are two fields where an answer is three.
	if len(fields) == 2 && fields[1] == "missing" {
		return Object{}, fmt.Errorf("%w: %s", ErrNoObject, name)
	}
	if len(fields) != 3 {
		return Object{}, b.fail(fmt.Errorf("unreadable answer for %s: %q", name, strings.TrimRight(header, "\n")))
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size < 0 {
		return Object{}, b.fail(fmt.Errorf("unreadable size for %s: %q", name, fields[2]))
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(b.out, data); err != nil {
		return Object{}, b.fail(err)
	}
	// git terminates every object's bytes with one newline of its own
	// framing, which is not the object's.
	if _, err := b.out.ReadByte(); err != nil {
		return Object{}, b.fail(err)
	}
	return Object{OID: fields[0], Type: fields[1], Data: data}, nil
}

// Tree reads one tree object and returns its entries in the order the
// tree records them, which is git's own sort and not the caller's.
//
// The entry format is binary — `<mode> <name>NUL<raw oid>` repeated —
// and the oid's width comes from the tree's OWN object name rather than
// from a constant, so a repository built with SHA-256 reads correctly
// without this package carrying a hash-algorithm switch.
func (b *Batch) Tree(name string) ([]TreeEntry, error) {
	obj, err := b.Object(name)
	if err != nil {
		return nil, err
	}
	if obj.Type != "tree" {
		return nil, fmt.Errorf("git cat-file: %s is a %s, not a tree", name, obj.Type)
	}
	width := len(obj.OID) / 2
	if width == 0 {
		return nil, fmt.Errorf("git cat-file: %s has no object name to size its entries by", name)
	}
	var entries []TreeEntry
	for rest := obj.Data; len(rest) > 0; {
		mode, after, ok := bytes.Cut(rest, []byte{' '})
		if !ok {
			return nil, fmt.Errorf("git cat-file: tree %s ends mid-entry", obj.OID)
		}
		entryName, after, ok := bytes.Cut(after, []byte{0})
		if !ok || len(after) < width {
			return nil, fmt.Errorf("git cat-file: tree %s ends mid-entry", obj.OID)
		}
		entries = append(entries, TreeEntry{
			Mode: string(mode),
			Name: string(entryName),
			OID:  fmt.Sprintf("%x", after[:width]),
		})
		rest = after[width:]
	}
	return entries, nil
}

// Close ends the session and reaps the process. A session the caller
// finished with exits zero on the EOF its closed stdin gives it; one
// that died carries its own words out of stderr, so a caller that
// closes without having read anything still learns why.
func (b *Batch) Close() error {
	if b.reaped {
		return b.broken
	}
	b.reaped = true
	closeErr := b.in.Close()
	waitErr := b.cmd.Wait()
	if closeErr != nil {
		return fmt.Errorf("git cat-file: %w", closeErr)
	}
	if waitErr != nil {
		if msg := strings.TrimSpace(b.errs.String()); msg != "" {
			return fmt.Errorf("git cat-file: %s", msg)
		}
		return fmt.Errorf("git cat-file: %w", waitErr)
	}
	return nil
}

// fail records the first transport failure and words it with whatever
// git said on stderr, which is the only place a session that died
// explains itself.
//
// The process is REAPED HERE, before stderr is read, and the order is
// not incidental: os/exec fills a non-file Stderr from a goroutine of
// its own, so those bytes belong to that goroutine until Wait has
// joined it — reading them earlier is a data race that the race
// detector is right to call one. Closing stdin first is what makes the
// Wait return promptly: git ends on the EOF, and a session already
// declared broken has no further request to send.
func (b *Batch) fail(err error) error {
	if b.broken != nil {
		return b.broken
	}
	b.broken = fmt.Errorf("git cat-file: %w", err)
	if !b.reaped {
		b.reaped = true
		_ = b.in.Close()
		_ = b.cmd.Wait()
	}
	if msg := strings.TrimSpace(b.errs.String()); msg != "" {
		b.broken = fmt.Errorf("git cat-file: %s", msg)
	}
	return b.broken
}
