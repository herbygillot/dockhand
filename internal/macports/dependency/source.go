package dependency

import (
	"context"
	"fmt"
	"path"
	"strings"
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
	_, member, err := Manifest(ctx, in.Archive, in.Worksrcdir, name)
	if err != nil {
		return err
	}
	if !rename && !GOPATHLayout(in.Worksrcdir) && member != path.Join(in.Worksrcdir, name) {
		return fmt.Errorf("%w: %s does not contain %s/%s", ErrManifestMissing, member, in.Worksrcdir, name)
	}
	return nil
}
