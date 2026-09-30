package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/herbygillot/dockhand/internal/atomicfile"
)

// ReaderVersion is the version of what a reading reads and how: raising it
// keeps a reading made before from standing for one made now. 2: a Node
// project's workspaces' package.json files.
const ReaderVersion = 2

// Cache keeps readings on disk by what they read: an archive's content, by
// its sha256, and where in it the project is, with the reader's version
// (the assessment design, B). The same archive read the same way is read
// once, and a reading kept for an archive's content stands without the
// archive, so an assessment made again needs no download. It's disposable:
// a reading that can't be kept, or found, is read again. The zero value
// keeps nothing.
type Cache struct {
	Directory string
}

// sha256Hex is a digest as a Portfile's checksums and a download's write
// it.
var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// file is where a reading of an archive's content is kept, and false
// where it can't be: no directory, or no digest to know it by.
func (c Cache) file(digest string, spec Spec) (string, bool) {
	if c.Directory == "" || !sha256Hex.MatchString(digest) {
		return "", false
	}
	key := sha256.Sum256(fmt.Appendf(nil, "%d\x00%s\x00%s", ReaderVersion, digest, spec.subdirectory()))
	return filepath.Join(c.Directory, hex.EncodeToString(key[:])+".json"), true
}

// Kept is the reading kept of an archive's content, read at spec, and
// whether there is one.
func (c Cache) Kept(digest string, spec Spec) (Reading, bool) {
	name, ok := c.file(digest, spec)
	if !ok {
		return Reading{}, false
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return Reading{}, false
	}
	var reading Reading
	if json.Unmarshal(data, &reading) != nil {
		return Reading{}, false
	}
	return reading, true
}

// Read reads an archive whose content's sha256 is digest, as Read does,
// from what's kept where it was read before, and keeps what it reads. An
// archive that can't be read is an error, never kept, so a reading tried
// again reads it again.
func (c Cache) Read(ctx context.Context, filename, digest string, spec Spec) (Reading, error) {
	if reading, ok := c.Kept(digest, spec); ok {
		return reading, nil
	}
	reading, err := Read(ctx, filename, spec)
	if err != nil {
		return Reading{}, err
	}
	if name, ok := c.file(digest, spec); ok {
		// Keeping it is a saving, not a promise: a reading that isn't kept
		// is read again.
		if data, err := json.Marshal(reading); err == nil && os.MkdirAll(c.Directory, 0o755) == nil {
			_ = atomicfile.Write(name, data, 0o644)
		}
	}
	return reading, nil
}
