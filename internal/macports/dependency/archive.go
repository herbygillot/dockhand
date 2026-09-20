package dependency

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"io"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/archive"
)

const maxManifestBytes = 16 << 20

// ErrManifestMissing is macports.ErrManifestMissing, which the forge reader
// in upstream reports as well.
var ErrManifestMissing = macports.ErrManifestMissing

// Manifest reads a regular archive member without extracting files onto the host.
// GOPATHLayout reports whether worksrcdir is the Go PortGroup's default,
// gopath/src/<go.package>, a post-extract location rather than an archive
// path.
func GOPATHLayout(worksrcdir string) bool {
	return strings.HasPrefix(strings.Trim(worksrcdir, "/"), "gopath/src/")
}

func Manifest(ctx context.Context, filename, worksrcdir, name string) ([]byte, string, error) {
	found := map[string][]byte{}
	err := archive.Walk(ctx, filename, func(member archive.Member) error {
		if path.Base(member.Name) != name {
			return nil
		}
		clean, ok := member.Clean()
		if !ok || !member.Regular {
			return fmt.Errorf("dependency: invalid archive member %s", member.Name)
		}
		if member.Size < 0 || member.Size > maxManifestBytes {
			return fmt.Errorf("dependency: oversized %s", name)
		}
		if _, exists := found[clean]; exists {
			return fmt.Errorf("dependency: duplicate archive member %s", member.Name)
		}
		data, err := io.ReadAll(io.LimitReader(member.Body, maxManifestBytes+1))
		if err != nil {
			return err
		}
		if len(data) > maxManifestBytes {
			return fmt.Errorf("dependency: oversized %s", name)
		}
		found[clean] = data
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	wanted := path.Join(worksrcdir, name)
	if data, ok := found[wanted]; ok {
		return data, wanted, nil
	}
	// A directory under gopath/src is where the Go PortGroup moves the
	// source after extraction, not where the archive holds it: the archive's
	// top-level directory is flattened into GOPATH by post-extract, so the
	// manifest sits directly under that top-level directory.
	_, subdir, _ := strings.Cut(strings.Trim(worksrcdir, "/"), "/")
	if GOPATHLayout(worksrcdir) {
		subdir = ""
	}
	relative := path.Join(subdir, name)
	var selected string
	for member := range found {
		_, suffix, nested := strings.Cut(member, "/")
		if (nested && suffix != relative) || (!nested && member != relative) {
			continue
		}
		if selected != "" {
			return nil, "", fmt.Errorf("dependency: multiple %s files; select an unambiguous worksrcdir", name)
		}
		selected = member
	}
	if selected == "" {
		return nil, "", fmt.Errorf("%w: source archive has no %s for %s", ErrManifestMissing, name, worksrcdir)
	}
	return found[selected], selected, nil
}
