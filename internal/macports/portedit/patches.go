package portedit

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// patched reports whether the evaluated port declares patch files.
func patched(info macports.PortInfo) bool {
	names, errs := syntax.ListValues(info.Options["patchfiles"])
	return len(errs) == 0 && len(names) > 0
}

// checkPatches records whether the port's patch files still apply to the
// downloaded candidate archives. It needs archives whose bytes were kept and
// the final evaluation, so callers run it while their archive store exists.
// A rejected patch is a finding on the result, never a preparation error.
func (s *Service) checkPatches(ctx context.Context, input *sourceInput, result *Result) error {
	if result.Prepared.Ports == nil {
		return nil
	}
	info := result.Prepared.Ports[input.target.Name]
	names, errs := syntax.ListValues(info.Options["patchfiles"])
	if len(errs) > 0 || len(names) == 0 {
		return nil
	}
	var archives []string
	for _, download := range result.Downloads {
		if download.Path != "" {
			archives = append(archives, download.Path)
		}
	}
	if len(archives) == 0 {
		return nil
	}
	root := info.Options["filespath"]
	if root == "" {
		root = filepath.Join(input.portdir(), "files")
	}
	patches := make([]patchcheck.Patch, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		patches = append(patches, patchcheck.Patch{Name: name, Data: data})
	}
	dir := info.Options["patch.dir"]
	if dir != "" && dir != "@worksrc@" && !strings.HasPrefix(dir, "@worksrc@/") {
		for _, patch := range patches {
			result.Patches = append(result.Patches, patchcheck.Result{Name: patch.Name, Detail: "patch.dir leaves the source directory"})
		}
		progress.Report(ctx, "Patches: %s", patchcheck.Summary(result.Patches))
		return nil
	}
	pre, _ := syntax.ListValues(info.Options["patch.pre_args"])
	rename := info.Options["extract.rename"]
	results, err := patchcheck.Check(ctx, patchcheck.Request{
		Archives: archives, Worksrcdir: filepath.ToSlash(info.Options["worksrcdir"]), Rename: rename == "yes" || rename == "true" || rename == "1",
		PatchDir: strings.TrimPrefix(strings.TrimPrefix(dir, "@worksrc@"), "/"), PreArgs: pre, Patches: patches,
	})
	if err != nil {
		return err
	}
	result.Patches = results
	if len(patchcheck.Rejected(results)) > 0 || len(results) > 0 && !results[0].Checked {
		progress.Report(ctx, "Patches: %s", patchcheck.Summary(results))
	} else {
		progress.VerboseReport(ctx, "Patches: %s", patchcheck.Summary(results))
	}
	return nil
}
