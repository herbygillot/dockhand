package depblock

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
)

// ConfirmSource checks archive contents before a generator is allowed to run.
// Without native renaming, the member must match the actual worksrcdir exactly.
func ConfirmSource(ctx context.Context, kind string, in Input, rename bool) error {
	name := "Cargo.lock"
	if kind == Go {
		name = "go.mod"
	}
	if in.Worksrcdir == "" || path.IsAbs(in.Worksrcdir) || path.Clean(in.Worksrcdir) != in.Worksrcdir || strings.HasPrefix(in.Worksrcdir, "../") {
		return fmt.Errorf("dependency: invalid source directory %q", in.Worksrcdir)
	}
	dir := in.Worksrcdir
	var member string
	var err error
	if kind == Go {
		_, member, err = Manifest(ctx, in.Archive, dir, name)
	} else {
		// A workspace member's lock is the workspace root's, above it.
		_, member, dir, err = CargoLock(ctx, in.Archive, dir)
	}
	if err != nil {
		return err
	}
	if !rename && !macports.GOPATHLayout(in.Worksrcdir) && member != path.Join(dir, name) {
		return fmt.Errorf("%w: %s does not contain %s/%s", macports.ErrManifestMissing, member, dir, name)
	}
	return nil
}
