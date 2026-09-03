package cargo

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
	"github.com/herbygillot/dockhand/internal/vendored"
)

// The cargo family's second block: cargo.crates_github, for crates the
// new version takes from a GitHub branch instead of the registry. The
// authority is the regenerated Cargo.lock — a git dependency can be
// INTRODUCED by the version being bumped to (field-measured on yazi,
// whose branch dropped the crate from cargo.crates and declared its
// git source nowhere, building a complete-looking Portfile that could
// not build). Each entry is {name owner/repo branch revision sha256},
// fetched by the PortGroup from github.com/<owner/repo>/archive/<rev>.tar.gz
// and wired into cargo's source replacement with the BRANCH name — so
// only branch-referenced git sources are expressible, and everything
// else declines by name.

// GithubSuppliedIn is the set of distfile names an evaluated
// cargo.crates_github option contributes, in the form vendored.Own
// subtracts: the PortGroup fetches each entry as
// ${cname}-${crevision}.tar.gz. The sibling of SuppliedIn, and needed
// for the same reason — a port with git crates would otherwise count
// their tarballs as its own distfiles and decline for checksums it
// was never going to rewrite.
func GithubSuppliedIn(option string) ([]string, error) {
	if option == "" {
		return nil, nil
	}
	words, errs := syntax.ListValues(option)
	if len(errs) != 0 {
		return nil, fmt.Errorf("%w: %s: %w", vendored.ErrMalformed, vendored.CargoCratesGithub, errs[0])
	}
	if len(words)%5 != 0 {
		return nil, fmt.Errorf("%w: %s holds %d words, not whole five-tuples",
			vendored.ErrMalformed, vendored.CargoCratesGithub, len(words))
	}
	names := make([]string, 0, len(words)/5)
	for i := 0; i < len(words); i += 5 {
		names = append(names, words[i]+"-"+words[i+3]+".tar.gz")
	}
	return names, nil
}

// gitCrate is one git-sourced package out of the lock.
type gitCrate struct {
	Name     string
	Repo     string // owner/project
	Branch   string
	Revision string // full commit
}

var gitSourceRE = regexp.MustCompile(`^git\+([^?#]+)(?:\?([^#]*))?#([0-9a-f]{7,40})$`)

// gitCrates reads the lock's git-sourced packages, declining any the
// block format cannot express: a non-GitHub host, or a tag/rev
// reference — the PortGroup's source replacement speaks branches.
func gitCrates(lock []byte) ([]gitCrate, error) {
	var out []gitCrate
	var name string
	for line := range strings.Lines(string(lock)) {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "name = "); ok {
			name = strings.Trim(v, `"`)
			continue
		}
		v, ok := strings.CutPrefix(line, "source = ")
		if !ok {
			continue
		}
		src := strings.Trim(v, `"`)
		if !strings.HasPrefix(src, "git+") {
			continue
		}
		m := gitSourceRE.FindStringSubmatch(src)
		if m == nil {
			return nil, &vendored.Decline{Kind: vendored.CargoCratesGithub,
				Detail: fmt.Sprintf("%s has a git source this tool cannot read: %s", name, src)}
		}
		url, query, rev := m[1], m[2], m[3]
		repo, ok := githubRepo(url)
		if !ok {
			return nil, &vendored.Decline{Kind: vendored.CargoCratesGithub,
				Detail: fmt.Sprintf("%s is vendored from %s; cargo.crates_github expresses GitHub sources only", name, url)}
		}
		branch, ok := strings.CutPrefix(query, "branch=")
		if !ok || branch == "" || strings.Contains(branch, "&") {
			return nil, &vendored.Decline{Kind: vendored.CargoCratesGithub,
				Detail: fmt.Sprintf("%s is pinned by %q; cargo.crates_github expresses branch references only", name, query)}
		}
		out = append(out, gitCrate{Name: name, Repo: repo, Branch: branch, Revision: rev})
	}
	return out, nil
}

func githubRepo(url string) (string, bool) {
	_, rest, ok := strings.Cut(url, "github.com/")
	if !ok {
		return "", false
	}
	rest = strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
	if strings.Count(rest, "/") != 1 {
		return "", false
	}
	return rest, true
}

// githubEdits realizes the crates_github half: fetch each pinned
// revision's tarball for its checksum, then replace, insert, or
// remove the block as the new lock demands.
func githubEdits(ctx context.Context, rc vendored.Regen, cratesSpan text.Span, crates []gitCrate) ([]edit.Edit, error) {
	span, located := locateGithubBlock(rc)

	if len(crates) == 0 {
		if !located {
			return nil, nil
		}
		// The new version dropped its git sources: the block goes, and
		// its separating blank line goes with it so no double blank stays.
		start := span.Start
		for n := 0; n < 2 && start > 0 && rc.Src[start-1] == '\n'; n++ {
			start--
		}
		return []edit.Edit{{Start: start, End: span.End,
			Old: string(rc.Src[start:span.End]), New: "", Reason: "drop cargo.crates_github"}}, nil
	}

	block, err := buildGithubBlock(ctx, rc, crates)
	if err != nil {
		return nil, err
	}
	if located {
		return []edit.Edit{{Start: span.Start, End: span.End,
			Old: span.Text(rc.Src), New: block, Reason: "regenerate cargo.crates_github"}}, nil
	}
	// Introduced by this version: the block is born beside its sibling.
	return []edit.Edit{{Start: cratesSpan.End, End: cratesSpan.End,
		Old: "", New: "\n\n" + block, Reason: "add cargo.crates_github"}}, nil
}

func locateGithubBlock(rc vendored.Regen) (text.Span, bool) {
	span, err := vendored.Locate(rc.Src, rc.CST, portstyle.ScopeOf(rc.Src, rc.Vals.Name), vendored.CargoCratesGithub)
	return span, err == nil
}

// buildGithubBlock fetches each revision's tarball — the checksum is
// of bytes actually downloaded, the same rule as every distfile — and
// renders the block in the tree's committed shape.
func buildGithubBlock(ctx context.Context, rc vendored.Regen, crates []gitCrate) (string, error) {
	if rc.Fetch == nil {
		return "", fmt.Errorf("vendored: no fetcher to checksum git crates with")
	}
	dir, remove, err := rc.TempDir.MakeDir("git-crates")
	if err != nil {
		return "", err
	}
	defer remove()
	var b strings.Builder
	b.WriteString("cargo.crates_github \\")
	for i, c := range crates {
		url := fmt.Sprintf("https://github.com/%s/archive/%s.tar.gz", c.Repo, c.Revision)
		dest := filepath.Join(dir, fmt.Sprintf("%s-%s.tar.gz", c.Name, c.Revision))
		sums, err := rc.Fetch.Fetch(ctx, []string{url}, distfile.Options{}, dest)
		if err != nil {
			return "", fmt.Errorf("vendored: fetching %s@%s: %w", c.Name, shortRev(c.Revision), err)
		}
		slog.Debug("checksummed git crate", "crate", c.Name, "rev", shortRev(c.Revision), "sha256", sums.Sha256)
		// The PortGroup imports the whole repository as one package
		// directory, so a repo whose root manifest is a workspace's
		// virtual manifest cannot feed cargo (field-measured on yazi:
		// the block regenerated, the build failed on "found a virtual
		// manifest instead of a package manifest"). The tarball is in
		// hand for its checksum, so the root manifest is judged here,
		// before any branch is minted.
		manifest, _, merr := distfile.Extract(ctx, rc.Tools, []string{dest}, "", "Cargo.toml")
		if merr != nil {
			return "", &vendored.Decline{Kind: vendored.CargoCratesGithub,
				Detail: fmt.Sprintf("%s's repository %s@%s carries no readable root Cargo.toml", c.Name, c.Repo, shortRev(c.Revision))}
		}
		if !packageManifest(manifest) {
			return "", &vendored.Decline{Kind: vendored.CargoCratesGithub,
				Detail: fmt.Sprintf("%s lives in a cargo workspace at %s; cargo.crates_github imports a repository as one package, and cargo refuses a virtual manifest", c.Name, c.Repo)}
		}
		fmt.Fprintf(&b, "\n    %s %s %s \\", c.Name, c.Repo, c.Branch)
		fmt.Fprintf(&b, "\n    %s \\", c.Revision)
		fmt.Fprintf(&b, "\n    %s", sums.Sha256)
		if i != len(crates)-1 {
			b.WriteString(" \\")
		}
	}
	return b.String(), nil
}

// packageManifest reports whether a root Cargo.toml declares a package,
// as opposed to a workspace's virtual manifest — the distinction cargo
// itself draws when reading a directory source.
func packageManifest(manifest []byte) bool {
	for line := range strings.Lines(string(manifest)) {
		line = strings.TrimSpace(line)
		if line == "[package]" || strings.HasPrefix(line, "[package]#") || strings.HasPrefix(line, "[package] ") {
			return true
		}
	}
	return false
}

// shortRev abbreviates a commit for messages; the lock's regex admits
// revisions already shorter than the abbreviation.
func shortRev(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}
