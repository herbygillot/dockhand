package upstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tool"
)

// ErrNoGit reports that the forge resolver needs git and the machine
// has none.
var ErrNoGit = errors.New("upstream: git not found on PATH")

// RawRef is one tag the forge holds, as the forge named it: the object
// id and the tag name with refs/tags/ stripped and nothing else done
// to it.
//
// The port's tag scheme is deliberately not applied here. Two ports on
// one repository — a port and its -devel subport, or two ports built
// from one monorepo — declare different prefixes and suffixes, and if
// the scheme were baked in they would be two observations of the same
// forge and cost two round trips. Stripped later, they are one.
type RawRef struct {
	Sha string `json:"sha"`
	Tag string `json:"tag"`
}

// Ref is one of a port's versions and the object it names.
type Ref struct {
	Version string `json:"version"`
	Sha     string `json:"sha"`
}

// Tags lists the versions of a forge's tags: one git ls-remote round
// trip, unauthenticated and unmetered, the same call for every git
// forge. Peeled duplicates are dropped, and the port's declared tag
// prefix and suffix are stripped — a tag not matching the scheme is
// not a version of this port and is excluded. git is resolved through
// the run's finder; GIT_TERMINAL_PROMPT=0 keeps a private repository
// from hanging on a credential prompt.
func Tags(ctx context.Context, tools *tool.Finder, r Repo) ([]string, error) {
	refs, err := TagRefs(ctx, tools, r)
	if err != nil {
		return nil, err
	}
	return Versions(refs), nil
}

// TagRefs is Tags with the object ids kept.
//
// The sha is worth carrying for two reasons that have nothing to do
// with each other. It is the exact object a bump would fetch, which is
// a column a report should have and a fact no version string carries.
// And a digest over the whole answer is the change detector a git
// remote gives us in place of an ETag: one ls-remote whose shas are
// unchanged means nothing upstream moved, so an observation derived
// from those tags — which releases the forge has cut, above all — is
// still good and need not be paid for again.
func TagRefs(ctx context.Context, tools *tool.Finder, r Repo) ([]Ref, error) {
	raw, err := LsRemote(ctx, tools, "", r.URL)
	if err != nil {
		return nil, err
	}
	return Scheme(raw, r), nil
}

// LsRemote asks a git remote for its tags, unauthenticated: the
// cheapest witness there is, and the only one every git forge answers
// the same way.
//
// agent is the User-Agent to identify dockhand with. It is a parameter
// rather than a constant because the running version is a fact about
// the run, known at the composition root and nowhere near here; an
// empty one sends git's own, which is what the single-port callers do
// because none of them has the version to hand and none of them makes
// enough requests for a host to care.
func LsRemote(ctx context.Context, tools *tool.Finder, agent, url string) ([]RawRef, error) {
	git, err := tools.Find(tool.Git)
	if err != nil {
		// Banded where it is raised, like every other way this witness
		// fails: one of the two resolvers cannot run, which is upstream's
		// answer being unavailable rather than the machine being unfit
		// for the work — the livecheck witness may still answer.
		return nil, &WitnessError{Witness: "git", Err: ErrNoGit}
	}
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if agent != "" {
		env = append(env, "GIT_HTTP_USER_AGENT="+agent)
	}
	out, _, err := tool.Output(ctx, git, tool.Opts{
		Args: []string{"ls-remote", "--tags", url},
		Env:  env,
	})
	if err != nil {
		return nil, lsRemoteFailed(url, err)
	}

	var refs []RawRef
	for line := range strings.Lines(string(out)) {
		sha, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		tag, ok := strings.CutPrefix(ref, "refs/tags/")
		if !ok || strings.HasSuffix(tag, "^{}") {
			continue
		}
		refs = append(refs, RawRef{Sha: sha, Tag: tag})
	}
	return refs, nil
}

// Scheme applies a port's declared tag scheme to a forge's tags: the
// prefix and suffix are stripped, and a tag matching neither is not a
// version of this port and is dropped.
func Scheme(raw []RawRef, r Repo) []Ref {
	var refs []Ref
	for _, rr := range raw {
		v, ok := strings.CutPrefix(rr.Tag, r.TagPrefix)
		if !ok {
			continue
		}
		if v, ok = strings.CutSuffix(v, r.TagSuffix); ok && v != "" {
			refs = append(refs, Ref{Version: v, Sha: rr.Sha})
		}
	}
	return refs
}

// Versions is the version column of a ref list, in the order the forge
// gave it and with its duplicates intact — two tags naming one version
// are two things the forge said.
func Versions(refs []Ref) []string {
	var out []string
	for _, r := range refs {
		out = append(out, r.Version)
	}
	return out
}

// ShaOf returns the object a version's tag names, empty when no tag
// yielded that version. Vercmp-equality rather than string equality,
// because the version a report resolved may be spelled the way
// livecheck spells it and the tag the way the forge does.
func ShaOf(refs []Ref, version string) string {
	if version == "" {
		return ""
	}
	for _, r := range refs {
		if r.Version == version {
			return r.Sha
		}
	}
	for _, r := range refs {
		if macports.VerCmp(r.Version, version) == 0 {
			return r.Sha
		}
	}
	return ""
}

// Digest fingerprints a forge's whole answer — the git remote's
// stand-in for an ETag.
//
// Sorted before hashing because ls-remote's output order is the
// server's and not a promise, and a digest that changed when a forge
// reordered its refs would report movement that did not happen. The
// object ids are hashed with the names: a tag moved to a different
// commit is upstream moving, even though the name list is identical.
func Digest(raw []RawRef) string {
	lines := make([]string, 0, len(raw))
	for _, r := range raw {
		lines = append(lines, r.Sha+"\t"+r.Tag)
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
