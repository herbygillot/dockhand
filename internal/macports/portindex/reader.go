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

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

var (
	errNoIndex    = errors.New("portindex: tree has no PortIndex")
	ErrNotIndexed = errors.New("portindex: name not indexed")
	errMalformed  = errors.New("portindex: malformed index")
)

// Entry contains the indexed metadata for one port or subport.
type Entry struct {
	Name    string
	Portdir string
	Fields  map[string]string
}

// Index reads names and metadata from one generated MacPorts PortIndex.
type Index struct {
	path      string
	offsets   map[string]int64
	fromQuick bool
	rescanned bool
}

// Open opens the generated index at root. PortIndex.quick is an optional accelerator.
func Open(root string) (*Index, error) {
	name := filepath.Join(root, portIndexName)
	if _, err := os.Stat(name); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", errNoIndex, root)
		}
		return nil, err
	}
	index := &Index{path: name}
	if offsets, err := readQuick(filepath.Join(root, quickIndexName)); err == nil {
		index.offsets, index.fromQuick = offsets, true
		return index, nil
	}
	offsets, err := scanOffsets(name)
	if err != nil {
		return nil, err
	}
	index.offsets = offsets
	return index, nil
}

func (i *Index) Len() int { return len(i.offsets) }

// Lookup resolves a name case-insensitively and repairs a stale quick index once.
func (i *Index) Lookup(name string) (Entry, error) {
	key := strings.ToLower(name)
	entry, err := i.lookup(key)
	if err != nil && i.fromQuick && !i.rescanned {
		offsets, scanErr := scanOffsets(i.path)
		if scanErr != nil {
			return Entry{}, scanErr
		}
		i.offsets, i.rescanned = offsets, true
		return i.lookup(key)
	}
	return entry, err
}

func (i *Index) lookup(key string) (Entry, error) {
	offset, ok := i.offsets[key]
	if !ok {
		return Entry{}, fmt.Errorf("%w: %s", ErrNotIndexed, key)
	}
	file, err := os.Open(i.path)
	if err != nil {
		return Entry{}, err
	}
	defer file.Close()
	if _, err = file.Seek(offset, io.SeekStart); err != nil {
		return Entry{}, err
	}
	frame, err := readFrame(bufio.NewReader(file))
	if err != nil {
		return Entry{}, err
	}
	if strings.ToLower(frame.name) != key {
		return Entry{}, fmt.Errorf("%w: offset for %q addresses %q", errMalformed, key, frame.name)
	}
	return frame.entry()
}

// each visits the complete index sequentially and stops when yield returns false.
func (i *Index) Each(yield func(Entry) bool) error {
	file, err := os.Open(i.path)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 1<<20)
	var offset int64
	for {
		frame, err := readFrame(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: at byte %d: %v", errMalformed, offset, err)
		}
		offset += frame.size
		entry, err := frame.entry()
		if err != nil {
			return err
		}
		if !yield(entry) {
			return nil
		}
	}
}

func readQuick(name string) (map[string]int64, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	offsets := map[string]int64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			return nil, fmt.Errorf("%w: invalid quick-index line", errMalformed)
		}
		offset, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || offset < 0 {
			return nil, fmt.Errorf("%w: invalid quick-index offset", errMalformed)
		}
		key := strings.ToLower(fields[0])
		if _, exists := offsets[key]; exists {
			return nil, fmt.Errorf("%w: duplicate quick-index name %q", errMalformed, fields[0])
		}
		offsets[key] = offset
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(offsets) == 0 {
		return nil, fmt.Errorf("%w: empty quick index", errMalformed)
	}
	return offsets, nil
}

func scanOffsets(name string) (map[string]int64, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 1<<20)
	offsets := map[string]int64{}
	var offset int64
	for {
		frame, err := readFrame(reader)
		if errors.Is(err, io.EOF) {
			return offsets, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: at byte %d: %v", errMalformed, offset, err)
		}
		key := strings.ToLower(frame.name)
		if _, exists := offsets[key]; exists {
			return nil, fmt.Errorf("%w: duplicate name %q", errMalformed, frame.name)
		}
		offsets[key] = offset
		offset += frame.size
	}
}

type frame struct {
	name    string
	payload []byte
	size    int64
}

func (f frame) entry() (Entry, error) {
	fields, failures := syntax.DictValues(string(f.payload))
	if len(failures) != 0 {
		return Entry{}, fmt.Errorf("%w: entry %q is not a Tcl dictionary", errMalformed, f.name)
	}
	entry := Entry{Name: f.name, Portdir: fields["portdir"], Fields: fields}
	if fields["name"] != "" {
		entry.Name = fields["name"]
	}
	if entry.Name == "" || entry.Portdir == "" {
		return Entry{}, fmt.Errorf("%w: entry %q lacks name or portdir", errMalformed, f.name)
	}
	return entry, nil
}

func readFrame(reader *bufio.Reader) (frame, error) {
	header, err := reader.ReadString('\n')
	if header == "" && err != nil {
		return frame{}, io.EOF
	}
	if err != nil {
		return frame{}, fmt.Errorf("truncated header")
	}
	values, failures := syntax.ListValues(strings.TrimSuffix(header, "\n"))
	if len(failures) != 0 || len(values) != 2 || values[0] == "" {
		return frame{}, fmt.Errorf("invalid header")
	}
	units, err := strconv.Atoi(values[1])
	if err != nil || units < 0 || units > maxPortIndexBytes {
		return frame{}, fmt.Errorf("invalid entry length %q", values[1])
	}
	payload, bytes, err := readUnits(reader, units)
	if err != nil {
		return frame{}, fmt.Errorf("truncated entry %q", values[0])
	}
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		if _, peekErr := reader.Peek(1); peekErr == nil {
			return frame{}, fmt.Errorf("entry %q does not end on a record boundary", values[0])
		}
	}
	return frame{name: values[0], payload: payload, size: int64(len(header)) + bytes}, nil
}

func readUnits(reader *bufio.Reader, units int) ([]byte, int64, error) {
	data := make([]byte, 0, units+units/8)
	for read := 0; read < units; {
		r, size, err := reader.ReadRune()
		if err != nil {
			return nil, 0, err
		}
		if r == utf8.RuneError && size == 1 {
			if err := reader.UnreadRune(); err != nil {
				return nil, 0, err
			}
			value, err := reader.ReadByte()
			if err != nil {
				return nil, 0, err
			}
			data = append(data, value)
			read++
			continue
		}
		data = utf8.AppendRune(data, r)
		if r > 0xffff {
			read += 2
		} else {
			read++
		}
		if read > units {
			return nil, 0, fmt.Errorf("length ends inside a UTF-16 surrogate pair")
		}
	}
	return data, int64(len(data)), nil
}
