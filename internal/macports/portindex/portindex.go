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

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// The index file (macports.IndexFile) holds the serialized entries: a
// "name length" header line, then length bytes of Tcl-list key/value
// payload, repeated. The accelerator (macports.IndexQuickFile) holds
// "lowercased-name offset" lines, each offset addressing an entry
// header in the index file.

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
	defer f.Close()
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
// offset, skipping payloads unparsed.
func scanOffsets(path string) (map[string]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	offsets := make(map[string]int64)
	var pos int64
	for {
		headerName, n, err := skipEntry(r)
		if errors.Is(err, io.EOF) {
			return offsets, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: at byte %d: %w", ErrMalformed, pos, err)
		}
		offsets[strings.ToLower(headerName)] = pos
		pos += n
	}
}

// readEntryAt reads and parses the entry whose header starts at off.
func readEntryAt(path string, off int64) (Entry, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return Entry{}, "", err
	}
	defer f.Close()
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
	headerName, payloadLen, _, err := readHeader(r)
	if err != nil {
		return Entry{}, "", err
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Entry{}, "", fmt.Errorf("%w: truncated entry %q", ErrMalformed, headerName)
	}
	fields, ferrs := syntax.DictValues(string(payload))
	if len(ferrs) > 0 {
		return Entry{}, "", fmt.Errorf("%w: entry %q payload does not split as a dict", ErrMalformed, headerName)
	}
	entry := Entry{Name: headerName, Portdir: fields["portdir"], Fields: fields}
	if n := fields["name"]; n != "" {
		entry.Name = n
	}
	return entry, headerName, nil
}

// skipEntry consumes one entry without parsing its payload, returning
// the header name and the bytes consumed including the payload's
// trailing newline.
func skipEntry(r *bufio.Reader) (string, int64, error) {
	headerName, payloadLen, headerLen, err := readHeader(r)
	if err != nil {
		return "", 0, err
	}
	if _, err := io.CopyN(io.Discard, r, int64(payloadLen)); err != nil {
		return "", 0, fmt.Errorf("truncated entry %q", headerName)
	}
	consumed := int64(headerLen) + int64(payloadLen)
	// The payload's trailing newline; absent only at a final entry
	// written without one.
	if nl, err := r.ReadByte(); err == nil {
		if nl != '\n' {
			return "", 0, fmt.Errorf("entry %q not newline-terminated", headerName)
		}
		consumed++
	}
	return headerName, consumed, nil
}
