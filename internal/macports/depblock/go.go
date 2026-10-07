package depblock

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/scratch"
)

// ModuleMoved is a go.mod whose module isn't the go.package the Portfile
// names: a module that moved, as pomo's did from GitHub to Codeberg, where
// the source is the version before the move.
type ModuleMoved struct{ From, To string }

func (m *ModuleMoved) Error() string {
	return fmt.Sprintf("dependency: go.mod's module is %s, where go.package is %s", m.From, m.To)
}

// GoModules are the modules go.vendors tokens declare, in their order.
func GoModules(values []string) ([]string, error) {
	rows, err := goRows(values)
	if err != nil {
		return nil, err
	}
	modules := make([]string, 0, len(rows))
	for _, row := range rows {
		modules = append(modules, row[0])
	}
	return modules, nil
}

func generateGo(ctx context.Context, executable string, in Input) (GeneratedBlocks, error) {
	data, member, err := Manifest(ctx, in.Archive, in.Worksrcdir, "go.mod")
	if err != nil {
		return GeneratedBlocks{}, err
	}
	if _, _, err := Manifest(ctx, in.Archive, in.Worksrcdir, "go.work"); err == nil {
		return GeneratedBlocks{}, fmt.Errorf("dependency: Go workspaces require manual preparation")
	} else if !errors.Is(err, macports.ErrManifestMissing) {
		return GeneratedBlocks{}, err
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return GeneratedBlocks{}, err
	}
	if len(mod.Replace) > 0 || len(mod.Exclude) > 0 {
		return GeneratedBlocks{}, fmt.Errorf("dependency: go.mod replace/exclude directives require manual preparation; go2port does not preserve them")
	}
	if mod.Module == nil {
		return GeneratedBlocks{}, fmt.Errorf("dependency: go.mod names no module")
	}
	if mod.Module.Mod.Path != in.Package {
		return GeneratedBlocks{}, &ModuleMoved{From: mod.Module.Mod.Path, To: in.Package}
	}
	if err := unprunedGraph(ctx, in, mod); err != nil {
		return GeneratedBlocks{}, err
	}
	if !safeToken(in.Package) || !safeToken(in.Tag) {
		return GeneratedBlocks{}, fmt.Errorf("dependency: invalid Go package or tag")
	}
	directory, err := scratch.Dir("go2port-")
	if err != nil {
		return GeneratedBlocks{}, err
	}
	defer os.RemoveAll(directory)
	subdir := path.Dir(member)
	if _, remaining, ok := strings.Cut(subdir, "/"); ok {
		subdir = remaining
	} else {
		subdir = "/"
	}
	output, err := run(ctx, executable, directory, "get", "--dir", subdir, "--", in.Package, in.Tag)
	if err != nil {
		return GeneratedBlocks{}, err
	}
	values, err := generated(output, Go)
	if err != nil {
		return GeneratedBlocks{}, err
	}
	rows, err := goRows(values)
	if err != nil {
		return GeneratedBlocks{}, err
	}
	expected := map[string]string{}
	for _, require := range mod.Require {
		version := semver.Canonical(require.Mod.Version)
		if module.IsPseudoVersion(require.Mod.Version) {
			version, err = module.PseudoVersionRev(require.Mod.Version)
			if err != nil {
				return GeneratedBlocks{}, err
			}
		}
		expected[require.Mod.Path] = version
	}
	actual := map[string]string{}
	for _, row := range rows {
		if _, ok := actual[row[0]]; ok {
			return GeneratedBlocks{}, fmt.Errorf("dependency: duplicate generated Go module")
		}
		for i := 1; i < len(row); i += 2 {
			if row[i] == "lock" {
				actual[row[0]] = LockVersion(row[0], row[i+1])
			}
		}
	}
	if !maps.Equal(actual, expected) {
		// go2port fetches the module by its path itself, which is where
		// pomo's, on codeberg.org, went wrong (field testing, 2026-10-02).
		// What differs is named, where the refusal said only that
		// something did (the rc8 full stage's B8, walk).
		return GeneratedBlocks{}, fmt.Errorf("dependency: go2port's output for %s %s does not cover the source go.mod requirements exactly (%s); go2port fetches the module from %s itself", in.Package, in.Tag, requirementDifferences(expected, actual), strings.SplitN(in.Package, "/", 2)[0])
	}
	return GeneratedBlocks{Values: map[string][]string{Go: values}}, nil
}

// LockVersion is the version a go.vendors lock names for a module. go2port
// locks a module in a subdirectory of its repository by the repository's
// tag, the subdirectory and the version, as github.com/charmbracelet/x/term
// at term/v0.2.1, where go.mod and go.sum name v0.2.1: walk 1.13.0's
// create was refused as not covering go.mod (the rc8 full stage's B8).
// Any other lock is its version as it is.
func LockVersion(modulePath, lock string) string {
	at := strings.LastIndex(lock, "/")
	if at < 0 {
		return lock
	}
	directory, version := lock[:at], lock[at+1:]
	path := modulePath
	if prefix, _, ok := module.SplitPathVersion(modulePath); ok {
		path = prefix
	}
	if !strings.HasSuffix(path, "/"+directory) {
		return lock
	}
	return version
}

// requirementDifferences names the modules go2port's output and go.mod
// disagree on: one missing, one extra, and one at another version.
func requirementDifferences(expected, actual map[string]string) string {
	var differences []string
	for _, path := range slices.Sorted(maps.Keys(expected)) {
		version, ok := actual[path]
		switch {
		case !ok:
			differences = append(differences, "missing "+path+" "+expected[path])
		case version != expected[path]:
			differences = append(differences, path+" at "+version+" where go.mod has "+expected[path])
		}
	}
	for _, path := range slices.Sorted(maps.Keys(actual)) {
		if _, ok := expected[path]; !ok {
			differences = append(differences, "extra "+path+" "+actual[path])
		}
	}
	return strings.Join(differences, "; ")
}

// unprunedGraph refuses a module whose go.mod says a Go before 1.17, or
// none: its go.mod needn't list every module its build reads, which go.sum
// does, and go2port writes go.mod's alone. countdown 1.5.0, at go 1.14,
// required go-runewidth, which imports rivo/uniseg, and its check failed at
// install with uniseg missing (the rc6 full stage, B8). The modules go.sum
// has the source of, and go.mod doesn't require, are named, but for one
// the Portfile's go.vendors already keeps by hand at a version go.sum
// pins, which the update keeps (KeepGoModules): go-reflex keeps kr/text
// v0.1.0 so, and every update of it was refused (the rc7 full stage).
func unprunedGraph(ctx context.Context, in Input, mod *modfile.File) error {
	if mod.Go != nil && semver.Compare("v"+mod.Go.Version, "v1.17") >= 0 {
		return nil
	}
	sum, _, err := Manifest(ctx, in.Archive, in.Worksrcdir, "go.sum")
	if errors.Is(err, macports.ErrManifestMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	required := map[string]bool{}
	for _, require := range mod.Require {
		required[require.Mod.Path] = true
	}
	// A declaration that doesn't read as go.vendors keeps nothing here;
	// the comparison with the generator's says what's wrong with it.
	vendored, _ := goRows(in.Vendored)
	for _, row := range vendored {
		if GoSumPins(sum, row[0], goField(row, "lock")) {
			required[row[0]] = true
		}
	}
	var missing []string
	for _, line := range strings.Split(string(sum), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasSuffix(fields[1], "/go.mod") || required[fields[0]] || slices.Contains(missing, fields[0]) {
			continue
		}
		missing = append(missing, fields[0])
	}
	if len(missing) == 0 {
		return nil
	}
	version := "no Go version"
	if mod.Go != nil {
		version = "go " + mod.Go.Version
	}
	slices.Sort(missing)
	return fmt.Errorf("dependency: go.mod says %s, before 1.17, so it needn't require every module its build reads, and go2port writes go.mod's alone; go.sum has the source of %s too, which go.vendors needs by hand", version, strings.Join(missing, ", "))
}

func goRows(values []string) ([][]string, error) {
	var rows [][]string
	for i := 0; i < len(values); {
		start := i
		if !strings.Contains(values[i], "/") {
			return nil, fmt.Errorf("dependency: invalid Go module declaration")
		}
		i++
		seen := map[string]bool{}
		for i < len(values) {
			key := values[i]
			if key != "repo" && key != "lock" && key != "sha256" && key != "rmd160" && key != "size" {
				break
			}
			if i+1 >= len(values) || seen[key] {
				return nil, fmt.Errorf("dependency: incomplete or duplicate Go field")
			}
			if key == "sha256" && !sha256Value(values[i+1]) {
				return nil, fmt.Errorf("dependency: missing generated Go checksum")
			}
			seen[key] = true
			i += 2
		}
		if !seen["lock"] || !seen["sha256"] {
			return nil, fmt.Errorf("dependency: Go module lacks version or checksum")
		}
		rows = append(rows, values[start:i])
	}
	return rows, nil
}
