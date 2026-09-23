package assess

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/herbygillot/dockhand/internal/record"
)

// Journal appends one JSON line per assessed port to a file as each port
// finishes, so a long run shows its progress in the file and an interrupted
// one continues: a rerun with the same file skips the ports it already
// holds. The first line names the source the ports were assessed from, and
// a rerun against another commit is refused, since its lines would not be
// comparable with the ones already there.
type Journal struct {
	path   string
	file   *os.File
	mu     sync.Mutex
	source *record.Source
	done   map[string]bool
}

// journalLine is what any line of the file can carry: the source, on the
// first line, or an assessed port, on every other.
type journalLine struct {
	Source   *record.Source `json:",omitempty"`
	Selector string         `json:",omitempty"`
}

// OpenJournal opens or creates the file and reads what it already holds.
func OpenJournal(path string) (*Journal, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	journal := &Journal{path: path, file: file, done: map[string]bool{}}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var parsed journalLine
		if err := json.Unmarshal(line, &parsed); err != nil {
			file.Close()
			return nil, fmt.Errorf("assess: journal %s: %w", path, err)
		}
		if parsed.Source != nil && journal.source == nil {
			journal.source = parsed.Source
		}
		if parsed.Selector != "" {
			journal.done[parsed.Selector] = true
		}
	}
	if err := scanner.Err(); err != nil {
		file.Close()
		return nil, fmt.Errorf("assess: journal %s: %w", path, err)
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
		return nil, err
	}
	return journal, nil
}

// Begin ties the journal to the source being assessed: a new file records
// it, and a file written against another commit is refused.
func (j *Journal) Begin(source record.Source) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.source != nil {
		if j.source.Commit != source.Commit {
			return fmt.Errorf("assess: journal %s holds an assessment of commit %s, not %s; use another file", j.path, j.source.Commit, source.Commit)
		}
		return nil
	}
	j.source = &source
	return j.write(journalLine{Source: &source})
}

// Has reports whether the journal already holds the port.
func (j *Journal) Has(selector string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done[selector]
}

// Record appends one assessed port.
func (j *Journal) Record(port Port) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.write(port); err != nil {
		return err
	}
	j.done[port.Selector] = true
	return nil
}

func (j *Journal) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = j.file.Write(append(data, '\n'))
	return err
}

func (j *Journal) Close() error {
	return j.file.Close()
}
