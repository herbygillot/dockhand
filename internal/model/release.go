package model

import (
	"cmp"
	"fmt"
	"net/url"
	"strings"
)

// EditIntent is what a person asked of an update beyond the version: the
// choices that shape the commit. It is set once from the command line and
// travels unchanged to the editor, so an option is one field rather than
// one copied through every type on the way.
type EditIntent struct {
	// SharedRelease authorizes moving every subport that shares the
	// selected port's release source.
	SharedRelease bool `json:",omitempty"`
	// Stub names the port a person selected when the edit targets its
	// newest versioned subport instead: the branch and commit carry this
	// name, and the editor honors the redirect.
	Stub string `json:",omitempty"`
	// KeepOldChecksums refreshes a legacy checksum group's values in place,
	// md5 and sha1 included, instead of rewriting it as rmd160, sha256, and
	// size.
	KeepOldChecksums bool `json:",omitempty"`
}

// ReleaseSelection records how a release was chosen, as distinct from what
// it is. Requested is the spelling a person typed, empty for automatic
// selection; CurrentVersion is what the selection was compared against,
// and NoUpdate says the port is already current. Stability classifies the
// selected version and LeavesStable marks a move from a stable current
// version to a prerelease; an explicit version is honored either way, and
// these only inform reporting. Embedded in Release, so its shape is flat.
type ReleaseSelection struct {
	Requested      string
	CurrentVersion string `json:",omitempty"`
	NoUpdate       bool   `json:",omitempty"`
	Stability      string `json:",omitempty"`
	LeavesStable   bool   `json:",omitempty"`
	// Lowers marks a version MacPorts' vercmp orders below the current
	// one, which `port upgrade` takes only where the epoch rises
	// (macports::_plan_upgrade), so the update raises it.
	Lowers bool `json:",omitempty"`
}

// ReleaseAsset is a file uploaded to a forge release, as the forge
// describes it: its name, where the forge serves it, and its size in
// bytes, SizeKnown false where the forge gave none, so an unknown size is
// told from an empty file.
type ReleaseAsset struct {
	Name      string
	URL       string
	Size      int64
	SizeKnown bool
}

// AssetNames are the assets' names, in their order.
func AssetNames(assets []ReleaseAsset) []string {
	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		names = append(names, asset.Name)
	}
	return names
}

// Release identifies the upstream source selected for a version bump: the
// frozen fact of forge, repository, tag, and commit, or an archive listing,
// together with the selection that produced it.
type Release struct {
	// Archive selects a Portfile version whose source is established by
	// evaluated download locations and prepared checksums, without a forge
	// tag.
	Archive bool            `json:",omitempty"`
	Listing *ReleaseListing `json:",omitempty"`
	ReleaseSelection
	Version string
	// SourceVersion is the version as the source spells it when the
	// Portfile derives its own from that spelling, such as a perl5 module
	// version that MacPorts normalizes into the port version; empty when
	// the two agree. It is what the version input is edited to; Version
	// is always the evaluated result.
	SourceVersion string `json:",omitempty"`
	// Pinned is a release that moves a commit pin, as wezterm's
	// github.setup names a commit: its Tag and Commit are the commit, which
	// is its SourceVersion too, and its Version the port's own literal.
	Pinned     bool `json:",omitempty"`
	Forge      string
	Instance   string
	Repository string
	Tag        string
	Commit     string
	// Page is the forge's page of the release, where its listing gave one,
	// and FromTag the tag of the version it updates from, where the
	// listing names it: what an update's commit body links (CommitBody).
	Page    string `json:",omitempty"`
	FromTag string `json:",omitempty"`
	// CapturedFrom is the prerelease's name a livecheck read the version
	// from, where it read it from the start of one, ap-utils's 1.4.2 from
	// 1.4.2pre1, which only a port on a prerelease takes: a distfile not
	// found for it says so. Empty for any other release.
	CapturedFrom string `json:",omitempty"`
}

// CapturedWords say where a version read from the start of a prerelease's
// name came from, for a distfile not found for it: "1.4.2 was read from
// 1.4.2pre1, a prerelease's name, by the port's livecheck"; empty for any
// other release.
func (r Release) CapturedWords() string {
	if r.CapturedFrom == "" {
		return ""
	}
	return fmt.Sprintf("%s was read from %s, a prerelease's name, by the port's livecheck", cmp.Or(r.SourceVersion, r.Version), r.CapturedFrom)
}

// CommitBody is what an update's commit says of its release beyond its
// subject, at best effort: the version it updates from, and a link to the
// release's page, else to the compare page between the two tags where
// both are known; empty where none of it is (the docs dig's I11). A
// reviewer looked each up by hand.
func (r Release) CommitBody() string {
	var lines []string
	if r.CurrentVersion != "" && r.CurrentVersion != r.Version {
		lines = append(lines, "Updates from "+r.CurrentVersion+".")
	}
	switch compare := r.compareURL(); {
	case r.Page != "":
		lines = append(lines, "Release: "+r.Page)
	case compare != "":
		lines = append(lines, "Changes: "+compare)
	}
	return strings.Join(lines, "\n")
}

// compareURL is the forge's page comparing the two tags, on GitHub or a
// GitLab instance; empty where either tag or the repository isn't known.
func (r Release) compareURL() string {
	if r.FromTag == "" || r.Tag == "" || r.Instance == "" || r.Repository == "" {
		return ""
	}
	base := strings.TrimRight(r.Instance, "/") + "/" + r.Repository
	switch r.Forge {
	case "github":
		return base + "/compare/" + url.PathEscape(r.FromTag) + "..." + url.PathEscape(r.Tag)
	case "gitlab":
		return base + "/-/compare/" + url.PathEscape(r.FromTag) + "..." + url.PathEscape(r.Tag)
	}
	return ""
}

// ReleaseListing retains compact discovery evidence; source archives are
// bound separately by their evaluated locations and prepared checksums.
type ReleaseListing struct {
	URL          string
	ETag         string `json:",omitempty"`
	LastModified string `json:",omitempty"`
	SHA256       string
}

// Names reports whether a Git fetch of a source fetches this release: its
// git.branch is the release's tag, or its commit where the Portfile pins
// one, and its git.url is the release's repository on its forge, as a
// forge PortGroup writes it, the repository's address with ".git" after
// it, whose case the forge doesn't read. An archive's release, or one
// found without a repository, names no Git source.
func (r Release) Names(source GitSource) bool {
	if r.Archive || r.Instance == "" || r.Repository == "" || source.Ref == "" {
		return false
	}
	if source.Ref != r.Tag && source.Ref != r.Commit {
		return false
	}
	address := strings.TrimSuffix(strings.TrimRight(source.URL, "/"), ".git")
	return strings.EqualFold(address, strings.TrimRight(r.Instance, "/")+"/"+r.Repository)
}

// SourceSpelling is the version the source names: SourceVersion when the
// Portfile derives its version from it, otherwise Version itself.
func (r Release) SourceSpelling() string {
	if r.SourceVersion != "" {
		return r.SourceVersion
	}
	return r.Version
}
