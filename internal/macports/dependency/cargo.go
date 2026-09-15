package dependency

import (
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"net/url"
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
	var git []GitCrate
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
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
				return GeneratedBlocks{}, fmt.Errorf("dependency: %s has an unsupported Git source", pkg.Name)
			}
			query, err := url.ParseQuery(u.RawQuery)
			repository := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
			branch := query.Get("branch")
			commit := u.Fragment
			if err != nil || len(query) != 1 || len(query["branch"]) != 1 || branch == "" || !safeToken(branch) || !safeRepository(repository) || len(commit) != 40 {
				return GeneratedBlocks{}, fmt.Errorf("dependency: %s requires a GitHub branch and full commit for cargo.crates_github", pkg.Name)
			}
			if _, err := hex.DecodeString(commit); err != nil {
				return GeneratedBlocks{}, fmt.Errorf("dependency: invalid Git crate commit")
			}
			crate := GitCrate{Name: pkg.Name, Repository: repository, Branch: branch, Commit: commit}
			if seenGit[crate.Distfile()] {
				return GeneratedBlocks{}, fmt.Errorf("dependency: ambiguous Git crate archive %s", crate.Distfile())
			}
			if old, ok := repoBranches[repository]; ok && old != branch {
				return GeneratedBlocks{}, fmt.Errorf("dependency: multiple branches of %s cannot share a Cargo source replacement", repository)
			}
			repoBranches[repository] = branch
			seenGit[crate.Distfile()] = true
			git = append(git, crate)
		} else {
			return GeneratedBlocks{}, fmt.Errorf("dependency: crate %s uses an unsupported registry or source", pkg.Name)
		}
	}
	directory, err := os.MkdirTemp("", "dockhand-cargo2port-")
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
	values, err := Generated(output, Cargo)
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
	return GeneratedBlocks{Values: map[string][]string{Cargo: values, CargoGit: nil}, Git: git}, nil
}

func (g GeneratedBlocks) WithGitChecksums(sums map[string]string) (map[string][]string, error) {
	values := maps.Clone(g.Values)
	for _, crate := range g.Git {
		sum := sums[crate.Distfile()]
		if !sha256Value(sum) {
			return nil, fmt.Errorf("dependency: missing checksum for %s", crate.Name)
		}
		values[CargoGit] = append(values[CargoGit], crate.Name, crate.Repository, crate.Branch, crate.Commit, sum)
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
