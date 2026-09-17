package dependency

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// Git reference kinds recorded by Cargo.lock source selectors.
const (
	GitBranch = "branch"
	GitTag    = "tag"
	GitRev    = "rev"
)

// GitReference is the selector a Cargo.lock Git source records for a crate.
// An empty Kind selects the repository's default branch.
type GitReference struct{ Kind, Value string }

func (r GitReference) String() string {
	if r.Kind == "" {
		return "default branch"
	}
	return r.Kind + " " + r.Value
}

// Declarable reports whether cargo.crates_github can express the reference.
// The cargo PortGroup writes the declared value as a branch into Cargo's source
// replacement, which matches the lockfile only for branch selectors.
func (r GitReference) Declarable() bool { return r.Kind == GitBranch }

// GitCrate identifies a Git-sourced crate by repository, exact commit, and selector.
type GitCrate struct {
	Name, Repository, Commit string
	Reference                GitReference
}

func (c GitCrate) Distfile() string { return c.Name + "-" + c.Commit + ".tar.gz" }

// GitPolicy states how a Cargo port obtains crates that Cargo.lock pins to Git.
type GitPolicy string

const (
	// GitDeclared builds offline, so cargo.crates_github must declare every Git crate.
	GitDeclared GitPolicy = "declared"
	// GitMixed resolves Git sources online at build time and declares the branch pins it can.
	GitMixed GitPolicy = "mixed"
	// GitOnline resolves every Git source online at build time and declares none.
	GitOnline GitPolicy = "online"
)

// gitPolicy derives the port's policy from its evaluated offline mode and whether
// it declares cargo.crates_github. An empty cargo.offline_cmd is the maintained
// workaround for Git sources the PortGroup cannot replace.
func gitPolicy(options map[string]string, declared bool) GitPolicy {
	offline, present := options["cargo.offline_cmd"]
	if !present || strings.TrimSpace(offline) != "" {
		return GitDeclared
	}
	if declared {
		return GitMixed
	}
	return GitOnline
}

// parseGitCrate reads one Cargo.lock Git source without deciding whether the
// port can declare it.
func parseGitCrate(name, raw string) (GitCrate, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
		return GitCrate{}, fmt.Errorf("dependency: %s has an unsupported Git source; cargo.crates_github supports GitHub repositories only", name)
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return GitCrate{}, fmt.Errorf("dependency: %s has an unsupported Git source selector", name)
	}
	crate := GitCrate{Name: name, Repository: strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), Commit: u.Fragment}
	if !safeRepository(crate.Repository) {
		return GitCrate{}, fmt.Errorf("dependency: %s has an unsupported Git repository path", name)
	}
	if data, err := hex.DecodeString(crate.Commit); err != nil || len(data) != 20 {
		return GitCrate{}, fmt.Errorf("dependency: %s does not record a full Git commit", name)
	}
	switch len(query) {
	case 0:
	case 1:
		for _, kind := range []string{GitBranch, GitTag, GitRev} {
			if values := query[kind]; len(values) == 1 && safeToken(values[0]) {
				crate.Reference = GitReference{Kind: kind, Value: values[0]}
			}
		}
		if crate.Reference.Kind == "" {
			return GitCrate{}, fmt.Errorf("dependency: %s has an unsupported Git source selector", name)
		}
	default:
		return GitCrate{}, fmt.Errorf("dependency: %s combines Git source selectors", name)
	}
	return crate, nil
}

// GitSummary names crates with their short commits for reports.
func GitSummary(crates []GitCrate) string {
	names := make([]string, len(crates))
	for i, crate := range crates {
		names[i] = crate.Name + "@" + crate.Commit[:8]
	}
	return strings.Join(names, ", ")
}
