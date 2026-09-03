package upstream

import (
	"context"
	"encoding/json"
	"strings"
)

// GhRunner runs one gh invocation — the authenticated channel the
// releases refinement needs. Declared here as a bare func type so the
// domain layer names the shape without importing the seam's owner;
// nil means "tags only", which is every caller without gh.
type GhRunner func(ctx context.Context, args ...string) (string, error)

// A GitHub repository's release versions are upstream's own
// authoritative word on what is a release and what is stable, which the
// tag heuristic can only approximate. flyctl field-proved the gap: bare
// calver CI tags (v2023.11.0) that look perfectly stable by name and
// were never releases at all.
//
// The witness itself is Manners.releases, and there is exactly one of
// it. There used to be two — a plain one here for the single port and a
// conditional, paced, cached one for the sweep — and the plain one
// answered "the call failed" and "this repository publishes no
// releases" with the same false. That is the difference between a
// forge that is rate-limiting dockhand and a tag-only repository, and
// collapsing them meant a rate-limited run silently judged every
// remaining port on the heuristic the feed exists to correct, with
// nothing walled and nothing said.

// releaseVersions reads a releases feed into this port's versions:
// drafts and anything upstream flagged prerelease are dropped, and the
// port's tag scheme is applied to what remains.
//
// false when the feed cannot stand in for the tag list — unparseable,
// or empty once filtered, which is the common and legitimate case of a
// repository that tags and never cuts a release.
//
// One copy, called by the single-port path above and by the staged
// observer, because it is the rule that decides which versions a
// verdict is reached over: two copies would be two answers to "what
// counts as a release", and the second one would drift silently.
func releaseVersions(body []byte, r Repo) ([]string, bool) {
	var rels []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if json.Unmarshal(body, &rels) != nil {
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
