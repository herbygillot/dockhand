package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// GhRunner runs one gh invocation — the authenticated channel the
// releases refinement needs. Declared here as a bare func type so the
// domain layer names the shape without importing the seam's owner;
// nil means "tags only", which is every caller without gh.
type GhRunner func(ctx context.Context, args ...string) (string, error)

// Releases lists a GitHub repository's release versions — upstream's
// own authoritative word on what is a release and what is stable,
// which the tag heuristic can only approximate. flyctl field-proved
// the gap: bare calver CI tags (v2023.11.0) that look perfectly
// stable by name and were never releases at all.
//
// ok is false when the answer cannot stand in for the tag list: not a
// GitHub repo, no gh, the call failed, or the repo simply publishes
// no releases (tag-only repos are common and legitimate). The caller
// falls back to tags.
func Releases(ctx context.Context, gh GhRunner, r Repo) ([]string, bool) {
	if gh == nil {
		return nil, false
	}
	owner, name, ok := githubPath(r.URL)
	if !ok {
		return nil, false
	}
	out, err := gh(ctx, "api", fmt.Sprintf("repos/%s/%s/releases?per_page=100", owner, name))
	if err != nil {
		return nil, false
	}
	var rels []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if json.Unmarshal([]byte(out), &rels) != nil {
		return nil, false
	}
	var versions []string
	for _, rel := range rels {
		if rel.Prerelease || rel.Draft {
			continue
		}
		v, ok := strings.CutPrefix(rel.TagName, r.TagPrefix)
		if !ok {
			continue
		}
		if v, ok = strings.CutSuffix(v, r.TagSuffix); ok && v != "" {
			versions = append(versions, v)
		}
	}
	if len(versions) == 0 {
		return nil, false
	}
	return versions, true
}

// githubPath reads owner and repository from a github.com URL, "" ok
// false for any other host — the releases API is GitHub's alone.
func githubPath(url string) (owner, name string, ok bool) {
	_, rest, found := strings.Cut(url, "github.com/")
	if !found {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimSuffix(rest, ".git"), "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
