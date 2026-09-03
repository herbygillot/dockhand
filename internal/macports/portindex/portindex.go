// Package portindex reads the PortIndex a ports tree carries at its
// root: the generated name→portdir map that MacPorts' own tooling
// (portindex, port) writes and consults. Every port gets an entry,
// subports included, which makes the index the resolution surface for
// names that have no directory of their own.
//
// The index is a cache of the tree (D8). Consumers treat it as a
// resolver hint and re-evaluate the Portfile it points at; a stale
// index misroutes a name at worst — it never becomes truth.
package portindex

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// The index file (macports.IndexFile) holds the serialized entries: a
// "name length" header line, then the Tcl-list key/value payload,
// repeated with no separator between them. The accelerator
// (macports.IndexQuickFile) holds "lowercased-name offset" lines, each
// offset addressing an entry header in the index file.
//
// Two things about the declared length are easy to get wrong, and both
// were, so they are written down here.
//
// It counts the payload's own trailing newline. There is no separator
// byte: the next entry's header begins immediately at headerEnd+length.
// Arithmetic against the accelerator's byte offsets on the maintainer's
// tree shows it three times running — quick puts r-a3 at 0, r-abm at
// 538 and r-acdm at 966, and the header "R-A3 529\n" is 9 bytes, so
// 0+9+529 = 538 and 538+10+418 = 966 exactly.
//
// The unit is not the byte. MacPorts writes Tcl's `string length`, a
// count of UTF-16 code units. Measured over that tree's whole index
// (41630 entries, 2026-09-01): 690 payloads hold non-ASCII text and one
// holds an astral character, and framing the file three ways walks
// 41630/41630 entries cleanly by UTF-16 unit, misreads one entry's name
// by code point, and dies at entry four by byte.
//
// Byte framing is what this package did before, which is why the
// accelerator was load-bearing rather than an accelerator: a tree
// whose quick file is stale or absent could not be read at all.

// ErrNoIndex reports a tree that carries no PortIndex at its root.
var ErrNoIndex = errors.New("portindex: tree has no PortIndex")

// ErrNotIndexed reports a name the index has no entry for.
var ErrNotIndexed = errors.New("portindex: name not indexed")

// ErrMalformed reports index content that does not follow the
// PortIndex format.
var ErrMalformed = errors.New("portindex: malformed index")

// Entry is one indexed port.
type Entry struct {
	Name    string            // canonical name, as indexed
	Portdir string            // slash path relative to the tree root, e.g. "sysutils/kubectl"
	Fields  map[string]string // the full indexed metadata, values as serialized
}

// Index resolves port names against one tree's PortIndex. Lookups are
// case-insensitive, matching MacPorts' own convention (the quick file's
// keys are pre-lowercased). Not safe for concurrent use.
type Index struct {
	path      string           // the PortIndex file
	offsets   map[string]int64 // lowercased name → entry header offset
	fromQuick bool             // offsets came from the quick file, unverified
	rescanned bool             // offsets rebuilt from the PortIndex itself
}

// Open reads the index of the tree rooted at treeRoot, preferring the
// quick accelerator when present and scanning the PortIndex otherwise.
func Open(treeRoot string) (*Index, error) {
	path := filepath.Join(treeRoot, macports.IndexFile)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoIndex, treeRoot)
	}
	ix := &Index{path: path}
	if offsets, err := readQuick(filepath.Join(treeRoot, macports.IndexQuickFile)); err == nil {
		ix.offsets, ix.fromQuick = offsets, true
		return ix, nil
	}
	offsets, err := scanOffsets(path)
	if err != nil {
		return nil, err
	}
	ix.offsets = offsets
	return ix, nil
}

// Len returns the number of indexed names.
func (ix *Index) Len() int { return len(ix.offsets) }

// Lookup resolves a port or subport name, case-insensitively. A quick
// accelerator that disagrees with the PortIndex it addresses (the pair
// can drift when only one is regenerated) triggers one rebuild of the
// offsets from the PortIndex itself before the lookup fails.
func (ix *Index) Lookup(name string) (Entry, error) {
	key := strings.ToLower(name)
	entry, err := ix.lookup(key)
	if err != nil && ix.fromQuick && !ix.rescanned {
		offsets, scanErr := scanOffsets(ix.path)
		if scanErr != nil {
			return Entry{}, scanErr
		}
		ix.offsets, ix.rescanned = offsets, true
		return ix.lookup(key)
	}
	return entry, err
}

func (ix *Index) lookup(key string) (Entry, error) {
	off, ok := ix.offsets[key]
	if !ok {
		return Entry{}, fmt.Errorf("%w: %s", ErrNotIndexed, key)
	}
	entry, headerName, err := readEntryAt(ix.path, off)
	if err != nil {
		return Entry{}, err
	}
	if strings.ToLower(headerName) != key {
		return Entry{}, fmt.Errorf("%w: offset for %q addresses %q", ErrMalformed, key, headerName)
	}
	return entry, nil
}

// readQuick parses the accelerator: one "name offset" pair per line.
func readQuick(path string) (map[string]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-path close: nothing was written
	offsets := make(map[string]int64)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			return nil, fmt.Errorf("%w: quick line %q", ErrMalformed, sc.Text())
		}
		off, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: quick offset %q", ErrMalformed, fields[1])
		}
		offsets[strings.ToLower(fields[0])] = off
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(offsets) == 0 {
		return nil, fmt.Errorf("%w: empty quick index", ErrMalformed)
	}
	return offsets, nil
}

// scanOffsets walks the whole PortIndex recording each entry's header
// offset, leaving payloads unparsed.
func scanOffsets(path string) (map[string]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-path close: nothing was written
	r := bufio.NewReaderSize(f, 1<<20)
	offsets := make(map[string]int64)
	var pos int64
	for {
		fr, err := readFramed(r)
		if errors.Is(err, io.EOF) {
			return offsets, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: at byte %d: %w", ErrMalformed, pos, err)
		}
		offsets[strings.ToLower(fr.name)] = pos
		pos += fr.size
	}
}

// Each visits every indexed entry in one sequential pass over the
// PortIndex, in the order the file holds them, until yield returns
// false. It is the enumeration a reverse index is built from: Lookup
// re-opens the file per call, so building one from 41630 lookups would
// be 41630 opens and seeks.
//
// An entry whose payload does not split as a dict is skipped and the
// walk continues — its framing is known, so its neighbours are still
// readable — but the error comes back at the end, once for the walk
// rather than once per entry. A caller building an index that must be
// complete, which is every caller there is, treats any returned error
// as "this tree cannot answer" and declines by name; a partial reverse
// index is a silently short cohort.
func (ix *Index) Each(yield func(Entry) bool) error {
	f, err := os.Open(ix.path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // read-path close: nothing was written
	r := bufio.NewReaderSize(f, 1<<20)
	var pos int64
	var walkErr error
	for {
		fr, err := readFramed(r)
		if errors.Is(err, io.EOF) {
			return walkErr
		}
		if err != nil {
			return fmt.Errorf("%w: at byte %d: %w", ErrMalformed, pos, err)
		}
		pos += fr.size
		entry, err := fr.entry()
		if err != nil {
			if walkErr == nil {
				walkErr = err
			}
			continue
		}
		if !yield(entry) {
			return walkErr
		}
	}
}

// readEntryAt reads and parses the entry whose header starts at off.
func readEntryAt(path string, off int64) (Entry, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return Entry{}, "", err
	}
	defer f.Close() //nolint:errcheck // read-path close: nothing was written
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return Entry{}, "", err
	}
	return readEntry(bufio.NewReader(f))
}

// readHeader parses one "name length" header line. A clean end of the
// index surfaces as io.EOF.
func readHeader(r *bufio.Reader) (name string, payloadLen int, headerLen int, err error) {
	header, err := r.ReadString('\n')
	if header == "" && err != nil {
		return "", 0, 0, io.EOF
	}
	if err != nil {
		return "", 0, 0, fmt.Errorf("truncated header %q", header)
	}
	vals, verrs := syntax.ListValues(strings.TrimSuffix(header, "\n"))
	if len(verrs) > 0 || len(vals) != 2 {
		return "", 0, 0, fmt.Errorf("bad header %q", header)
	}
	n, err := strconv.Atoi(vals[1])
	if err != nil || n < 0 {
		return "", 0, 0, fmt.Errorf("bad entry length %q", vals[1])
	}
	return vals[0], n, len(header), nil
}

// readEntry parses one full entry at the reader's position.
func readEntry(r *bufio.Reader) (Entry, string, error) {
	fr, err := readFramed(r)
	if err != nil {
		return Entry{}, "", err
	}
	entry, err := fr.entry()
	if err != nil {
		return Entry{}, "", err
	}
	return entry, fr.name, nil
}

// framed is one entry located in the file: the name its header
// declares, the payload bytes, and the total bytes the entry occupies
// from its header to the next entry's.
type framed struct {
	name    string
	payload []byte
	size    int64
}

// entry parses the payload as the Tcl dict it is. The entry's canonical
// name is the payload's own `name` field where it has one — the header
// repeats it, but the payload is what portindex wrote the port as.
func (fr framed) entry() (Entry, error) {
	fields, ferrs := syntax.DictValues(string(fr.payload))
	if len(ferrs) > 0 {
		return Entry{}, fmt.Errorf("%w: entry %q payload does not split as a dict", ErrMalformed, fr.name)
	}
	entry := Entry{Name: fr.name, Portdir: fields["portdir"], Fields: fields}
	if n := fields["name"]; n != "" {
		entry.Name = n
	}
	return entry, nil
}

// readFramed consumes one entry — header and payload — from r. A clean
// end of the index surfaces as io.EOF.
func readFramed(r *bufio.Reader) (framed, error) {
	name, payloadLen, headerLen, err := readHeader(r)
	if err != nil {
		return framed{}, err
	}
	payload, n, err := readPayload(r, payloadLen)
	if err != nil {
		return framed{}, fmt.Errorf("%w: truncated entry %q", ErrMalformed, name)
	}
	// Every payload ends in its own newline — 41630 of 41630 on the
	// tree measured above — so a frame that lands anywhere else means
	// the declared length was not counted in UTF-16 units, which is
	// what a tclsh counting code points or bytes would write. Refusing
	// here is the point: the alternative is reading the next entry's
	// name with its first character eaten, which is what code-point
	// framing does to x86_64-w64-mingw32-gcc-bootstrap and reports as
	// success. The remedy is to regenerate the tree's PortIndex.
	//
	// A final entry written without the newline is not a misframe —
	// nothing follows it to be misread — so the check asks whether
	// anything does.
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		if _, peekErr := r.Peek(1); peekErr == nil {
			return framed{}, fmt.Errorf("%w: entry %q declares %d units that do not end at a newline", ErrMalformed, name, payloadLen)
		}
	}
	return framed{name: name, payload: payload, size: int64(headerLen) + n}, nil
}

// readPayload consumes units UTF-16 code units from r and returns the
// bytes they occupy. Bytes that are not valid UTF-8 are passed through
// as themselves and counted as one unit each, rather than folded into a
// replacement character: what the file holds is what a consumer of the
// payload has to see.
func readPayload(r *bufio.Reader, units int) ([]byte, int64, error) {
	out := make([]byte, 0, units+units/8)
	for n := 0; n < units; {
		c, size, err := r.ReadRune()
		if err != nil {
			return nil, 0, err
		}
		if c == utf8.RuneError && size == 1 {
			if err := r.UnreadRune(); err != nil {
				return nil, 0, err
			}
			b, err := r.ReadByte()
			if err != nil {
				return nil, 0, err
			}
			out = append(out, b)
			n++
			continue
		}
		out = utf8.AppendRune(out, c)
		// An astral character is a surrogate pair to Tcl, and two units.
		if c > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return out, int64(len(out)), nil
}
