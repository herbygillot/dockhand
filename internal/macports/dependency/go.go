package dependency

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

func generateGo(ctx context.Context, executable string, in Input) (GeneratedBlocks, error) {
	data, member, err := Manifest(ctx, in.Archive, in.Worksrcdir, "go.mod")
	if err != nil {
		return GeneratedBlocks{}, err
	}
	if _, _, err := Manifest(ctx, in.Archive, in.Worksrcdir, "go.work"); err == nil {
		return GeneratedBlocks{}, fmt.Errorf("dependency: Go workspaces require manual preparation")
	} else if !errors.Is(err, ErrManifestMissing) {
		return GeneratedBlocks{}, err
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return GeneratedBlocks{}, err
	}
	if len(mod.Replace) > 0 || len(mod.Exclude) > 0 {
		return GeneratedBlocks{}, fmt.Errorf("dependency: go.mod replace/exclude directives require manual preparation; go2port does not preserve them")
	}
	if mod.Module == nil || mod.Module.Mod.Path != in.Package {
		return GeneratedBlocks{}, fmt.Errorf("dependency: go.mod module does not match go.package")
	}
	if !safeToken(in.Package) || !safeToken(in.Tag) {
		return GeneratedBlocks{}, fmt.Errorf("dependency: invalid Go package or tag")
	}
	directory, err := os.MkdirTemp("", "dockhand-go2port-")
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
				actual[row[0]] = row[i+1]
			}
		}
	}
	if !maps.Equal(actual, expected) {
		return GeneratedBlocks{}, fmt.Errorf("dependency: go2port output does not cover the source go.mod requirements exactly")
	}
	return GeneratedBlocks{Values: map[string][]string{Go: values}}, nil
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
