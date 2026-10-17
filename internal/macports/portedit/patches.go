package portedit

import (
	"context"
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
	// MacPorts patches what it extracted, which a distfile extract.only
	// leaves out isn't.
	for _, download := range result.Downloads {
		if download.Path != "" && info.Extracts(download.Name) {
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
	// Nothing but the patch list changes, held to the whole family as the
	// other corrections are: a setting that reads the list, as
	// use_autoreconf inside if {[llength ${patchfiles}] > 0}, or a sibling
	// sharing it, moving is said, and the patches stay (Codex's review of
	// 832f059e, high 3).
	before, _ := syntax.ListValues(info.Options["patchfiles"])
	wanted := slices.DeleteFunc(slices.Clone(before), func(p string) bool { return slices.Contains(dropped, p) })
	report, applied, err := s.correct(ctx, input, result, correction{contents: contents, deletes: files, expected: []string{name + ".patchfiles -" + strings.Join(dropped, " -")},
		allowed: func(port string, prepared, corrected macports.PortInfo) macports.PortInfo {
			if port != name {
				return prepared
			}
			// The list as evaluated, where it's the list less those
			// dropped, however Tcl spaces it.
			prepared.Options["patchfiles"] = strings.Join(wanted, " ")
			value, set := corrected.Options["patchfiles"]
			if actual, err := syntax.ListValues(value); err == nil && slices.Equal(actual, wanted) {
				prepared.Options["patchfiles"] = value
				if !set {
					// None left, and the option with them.
					delete(prepared.Options, "patchfiles")
				}
			}
			return prepared
		}})
	if err != nil || !applied {
		if !applied && err == nil {
			progress.Report(ctx, "Kept %s, which the new source already holds: dropping %s also changes %s", strings.Join(dropped, ", "), map[bool]string{true: "it", false: "them"}[len(dropped) == 1], strings.Join(report.UnexpectedChanges, "; "))
		}
		return nil
	}
	result.Dropped = dropped
	progress.Report(ctx, "Dropped %s, which the new source already holds", strings.Join(dropped, ", "))
	return nil
}
