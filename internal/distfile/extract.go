package distfile

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/tool"
)

// maxMemberSize bounds what is read out of an archive. The files worth
// reading this way — lockfiles, manifests — run to a few hundred
// kilobytes; the cap guards against a pathological distfile, not a real
// one.
const maxMemberSize = 32 << 20

var (
	// ErrMemberMissing reports that no candidate distfile carries the file
	// sought. Some projects do not commit the file a caller needs, and
	// some forges' release tarballs omit it, so this is an ordinary reason
	// to decline a port rather than a failure.
	ErrMemberMissing = errors.New("distfile: file not found in any distfile")
	// ErrMemberAmbiguous reports several equally shallow candidates within one
	// archive, with no directory given to choose between them.
	ErrMemberAmbiguous = errors.New("distfile: several candidates and no way to choose")
)

// Extract reads the named file out of a set of fetched distfiles,
// returning its contents and the distfile that carried it. Candidates
// are tried in order and the first that yields the file wins.
//
// The archiver is tool.Tar, resolved through the run's finder: the
// libarchive tar reads every format a distfile arrives in — gzip,
// bzip2, xz, zip — so this package drives one tool rather than
// carrying format handling of its own. A machine without it is
// reported as the finder words it, before any candidate is tried.
//
// The candidates are whatever a port fetches for itself, and not all of
// them are archives — ports fetch loose man pages as distfiles. So
// extraction is the test rather than a guess from a file name: a
// candidate that cannot even be listed is skipped, not an error.
//
// preferDir is the directory the port evaluates its source into, which
// picks between copies when an archive holds more than one. Reading from
// bytes already fetched, rather than downloading a second time, is what
// lets a caller claim that what it read and the checksum it recorded
// describe the same artifact.
func Extract(ctx context.Context, tools *tool.Finder, archives []string, preferDir, name string) (data []byte, from string, err error) {
	tar, err := tools.Find(tool.Tar)
	if err != nil {
		return nil, "", err
	}
	var why []string
	for _, archive := range archives {
		base := path.Base(archive)
		names, err := members(ctx, tar, archive)
		if err != nil {
			why = append(why, base+": not an archive")
			continue
		}
		member, err := pickMember(names, preferDir, name)
		if err != nil {
			why = append(why, base+": "+err.Error())
			continue
		}
		body, err := extract(ctx, tar, archive, member)
		if err != nil {
			return nil, "", err
		}
		if len(body) == 0 {
			why = append(why, base+": "+member+" is empty")
			continue
		}
		return body, archive, nil
	}
	return nil, "", fmt.Errorf("%w: %s, tried %d (%s)", ErrMemberMissing, name, len(archives), strings.Join(why, "; "))
}

// members lists an archive's entries.
func members(ctx context.Context, tar, archive string) ([]string, error) {
	out, _, err := tool.Output(ctx, tar, tool.Opts{Args: []string{"-tf", archive}})
	if err != nil {
		return nil, fmt.Errorf("distfile: listing %s: %s", path.Base(archive), err) //nolint:errorlint // not wrapped: the child's words survive as text and its identity does not; a child's exit status is not dockhand's to hand on
	}
	var names []string
	for line := range strings.Lines(string(out)) {
		if n := strings.TrimRight(strings.TrimSuffix(line, "\n"), "/"); n != "" {
			names = append(names, strings.TrimPrefix(n, "./"))
		}
	}
	return names, nil
}

// pickMember chooses which entry is the file sought: the one under
// preferDir if the port named a directory, else the shallowest, which
// must then be unambiguous. Depth breaks ties because a vendored or test
// fixture copy always sits below the project's own.
func pickMember(members []string, preferDir, name string) (string, error) {
	want := path.Join(preferDir, name)
	best, bestDepth, ties := "", -1, 0
	for _, m := range members {
		if preferDir != "" && m == want {
			return m, nil
		}
		if path.Base(m) != name {
			continue
		}
		switch d := strings.Count(m, "/"); {
		case bestDepth < 0 || d < bestDepth:
			best, bestDepth, ties = m, d, 1
		case d == bestDepth:
			ties++
		}
	}
	switch {
	case best == "":
		return "", ErrMemberMissing
	case ties > 1:
		return "", fmt.Errorf("%w: %d at depth %d", ErrMemberAmbiguous, ties, bestDepth)
	}
	return best, nil
}

// extract reads one member's bytes out of an archive.
func extract(ctx context.Context, tar, archive, member string) ([]byte, error) {
	out, _, err := tool.Output(ctx, tar, tool.Opts{Args: []string{"-xOf", archive, member}})
	if err != nil {
		return nil, fmt.Errorf("distfile: extracting %s from %s: %s", member, path.Base(archive), err) //nolint:errorlint // not wrapped: the child's words survive as text and its identity does not; a child's exit status is not dockhand's to hand on
	}
	if len(out) > maxMemberSize {
		return nil, fmt.Errorf("distfile: %s is %d bytes, past the %d byte cap", member, len(out), maxMemberSize)
	}
	return out, nil
}
