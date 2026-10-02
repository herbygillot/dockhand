package portedit

import (
	"context"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
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
	results, err := patchcheck.Port(ctx, info, archives, patches)
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

// dropMergedPatches takes out of the Portfile the patches the new source
// already holds, and their files: what a maintainer does when upstream
// merges one, which about forty of the tree's updates meet (the roadmap's
// item 7). Only a patch written as a literal word of the top-level
// patchfiles, in files/, is dropped; the port must evaluate as before but
// for those patches. One it can't drop stays a rejected patch, said.
func (s *Service) dropMergedPatches(ctx context.Context, input *sourceInput, result *Result) error {
	if len(result.Files) == 0 || result.Prepared.Ports == nil {
		return nil
	}
	name := input.target.Name
	info := result.Prepared.Ports[name]
	directory := path.Dir(input.target.Portfile)
	contents := result.Files[0].After
	var dropped, files []string
	for _, patch := range result.Patches {
		if !patch.Merged {
			continue
		}
		file, below := info.FilesPath(directory, patch.Name)
		if !below {
			continue
		}
		if next, ok := portfile.DropPatch(contents, patch.Name); ok {
			contents, dropped, files = next, append(dropped, patch.Name), append(files, file)
		}
	}
	if len(dropped) == 0 {
		return nil
	}
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return nil
	}
	// Nothing but the patches changes: the port as prepared, less them.
	expected := info
	expected.Options = maps.Clone(info.Options)
	before, _ := syntax.ListValues(info.Options["patchfiles"])
	expected.Options["patchfiles"] = strings.Join(slices.DeleteFunc(slices.Clone(before), func(p string) bool { return slices.Contains(dropped, p) }), " ")
	after := evaluated.after.Ports[name]
	actual, _ := syntax.ListValues(after.Options["patchfiles"])
	wanted, _ := syntax.ListValues(expected.Options["patchfiles"])
	if !slices.Equal(actual, wanted) || after.Version != info.Version || after.Revision != info.Revision {
		return nil
	}
	result.Files[0] = evaluated.edit
	for _, file := range files {
		result.Files = append(result.Files, portfile.Edit{Path: file, Delete: true})
	}
	result.Prepared = evaluated.after
	result.Dropped = dropped
	progress.Report(ctx, "Dropped %s, which the new source already holds", strings.Join(dropped, ", "))
	return nil
}
