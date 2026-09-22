package dependency

import (
	"cmp"
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/scratch"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/semver"
)

type cargoPackage struct{ Name, Version, Source, Checksum string }
type cargoLock struct {
	Version int
	Package []cargoPackage
}

func generateCargo(ctx context.Context, executable string, in Input) (GeneratedBlocks, error) {
	data, _, err := Manifest(ctx, in.Archive, in.Worksrcdir, "Cargo.lock")
	if err != nil {
		return GeneratedBlocks{}, err
	}
	var lock cargoLock
	if _, err := toml.Decode(string(data), &lock); err != nil {
		return GeneratedBlocks{}, fmt.Errorf("dependency: Cargo.lock: %w", err)
	}
	if lock.Version < 1 || lock.Version > 4 || len(lock.Package) == 0 {
		return GeneratedBlocks{}, fmt.Errorf("dependency: unsupported or empty Cargo.lock")
	}
	expected := map[string]string{}
	var git, online []GitCrate
	seenGit := map[string]bool{}
	repoBranches := map[string]string{}
	for _, pkg := range lock.Package {
		if !crateName(pkg.Name) || !semver.IsValid("v"+pkg.Version) {
			return GeneratedBlocks{}, fmt.Errorf("dependency: invalid Cargo.lock package")
		}
		if pkg.Source == "" {
			continue
		}
		if pkg.Source == "registry+https://github.com/rust-lang/crates.io-index" || pkg.Source == "sparse+https://index.crates.io/" {
			if !sha256Value(pkg.Checksum) {
				return GeneratedBlocks{}, fmt.Errorf("dependency: registry crate %s has no valid checksum", pkg.Name)
			}
			key := pkg.Name + " " + pkg.Version
			if _, ok := expected[key]; ok {
				return GeneratedBlocks{}, fmt.Errorf("dependency: duplicate registry crate %s", key)
			}
			expected[key] = pkg.Checksum
		} else if raw, ok := strings.CutPrefix(pkg.Source, "git+"); ok {
			crate, err := parseGitCrate(pkg.Name, raw)
			if err != nil {
				return GeneratedBlocks{}, err
			}
			if !crate.Reference.declarable() || in.Git == GitOnline {
				if in.Git != GitOnline && in.Git != GitMixed {
					return GeneratedBlocks{}, fmt.Errorf("dependency: %s is pinned to Git %s; cargo.crates_github declares branches only, so an offline build cannot resolve it: pin a branch upstream, or let the port resolve Git sources online with an empty cargo.offline_cmd", pkg.Name, crate.Reference)
				}
				online = append(online, crate)
				continue
			}
			if seenGit[crate.Distfile()] {
				return GeneratedBlocks{}, fmt.Errorf("dependency: ambiguous Git crate archive %s", crate.Distfile())
			}
			if old, ok := repoBranches[crate.Repository]; ok && old != crate.Reference.Value {
				return GeneratedBlocks{}, fmt.Errorf("dependency: multiple branches of %s cannot share a Cargo source replacement", crate.Repository)
			}
			repoBranches[crate.Repository] = crate.Reference.Value
			seenGit[crate.Distfile()] = true
			git = append(git, crate)
		} else {
			return GeneratedBlocks{}, fmt.Errorf("dependency: crate %s uses an unsupported registry or source", pkg.Name)
		}
	}
	directory, err := scratch.Dir("cargo2port-")
	if err != nil {
		return GeneratedBlocks{}, err
	}
	defer os.RemoveAll(directory)
	filename := filepath.Join(directory, "Cargo.lock")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		return GeneratedBlocks{}, err
	}
	output, err := run(ctx, executable, directory, filename)
	if err != nil {
		return GeneratedBlocks{}, err
	}
	values, err := generated(output, Cargo)
	if err != nil {
		return GeneratedBlocks{}, err
	}
	if len(values)%3 != 0 {
		return GeneratedBlocks{}, fmt.Errorf("dependency: cargo2port returned incomplete crate triples")
	}
	actual := map[string]string{}
	for i := 0; i < len(values); i += 3 {
		key := values[i] + " " + values[i+1]
		if _, exists := actual[key]; exists {
			return GeneratedBlocks{}, fmt.Errorf("dependency: cargo2port returned duplicate crates")
		}
		actual[key] = values[i+2]
	}
	if !maps.Equal(actual, expected) {
		return GeneratedBlocks{}, fmt.Errorf("dependency: cargo2port output does not cover Cargo.lock registry checksums exactly")
	}
	slices.SortFunc(git, func(a, b GitCrate) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(online, func(a, b GitCrate) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Commit, b.Commit))
	})
	return GeneratedBlocks{Values: map[string][]string{Cargo: values, CargoGit: nil}, Git: git, Online: online}, nil
}

func (g GeneratedBlocks) WithGitChecksums(sums map[string]string) (map[string][]string, error) {
	values := maps.Clone(g.Values)
	for _, crate := range g.Git {
		sum := sums[crate.Distfile()]
		if !sha256Value(sum) {
			return nil, fmt.Errorf("dependency: missing checksum for %s", crate.Name)
		}
		values[CargoGit] = append(values[CargoGit], crate.Name, crate.Repository, crate.Reference.Value, crate.Commit, sum)
	}
	return values, nil
}
func safeRepository(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
				return false
			}
		}
	}
	return true
}

func crateName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
