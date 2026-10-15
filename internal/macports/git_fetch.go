package macports

import (
	"errors"

	"github.com/herbygillot/dockhand/internal/model"
)

// GitFetched reports whether MacPorts fetches the port by cloning it with
// Git, as its evaluated fetch.type says; false where that couldn't be
// settled.
func (p PortInfo) GitFetched() bool {
	value, _, err := p.option("fetch.type")
	return err == nil && value == "git"
}

// GitSource is what a Git-fetched port declares it fetches, as the
// Portfile reference has it: git.url, the repository Base clones, and
// git.branch, the tag, branch, or commit it checks out after, empty for
// the repository's default branch. git is false for a port fetched
// otherwise; an error where the evaluation couldn't settle which, or
// from where.
func (p PortInfo) GitSource() (source model.GitSource, git bool, err error) {
	fetch, _, err := p.option("fetch.type")
	if err != nil || fetch != "git" {
		return model.GitSource{}, false, err
	}
	url, _, err := p.option("git.url")
	if err != nil {
		return model.GitSource{}, true, err
	}
	if url == "" {
		return model.GitSource{}, true, errors.New("macports: fetched with Git, and git.url names no repository")
	}
	branch, _, err := p.option("git.branch")
	if err != nil {
		return model.GitSource{}, true, err
	}
	return model.GitSource{URL: url, Ref: branch}, true, nil
}

// GitHubTag is the GitHub tag a port fetched as an archive of a tag takes
// its source from, as the github PortGroup's github.setup sets it: git.url,
// the project's repository, and git.branch, the tag it makes of the
// version. ok is false for a port fetched with Git, one not set up from
// GitHub, and one whose github.tarball_from fetches an uploaded release
// asset, which a tag's commit doesn't make.
func (p PortInfo) GitHubTag() (source model.GitSource, ok bool) {
	fetch, _, err := p.option("fetch.type")
	if err != nil || fetch == "git" {
		return model.GitSource{}, false
	}
	author, _, err := p.option("github.author")
	if err != nil || author == "" {
		return model.GitSource{}, false
	}
	from, _, err := p.option("github.tarball_from")
	if err != nil || from != "" && from != "tarball" && from != "tags" && from != "archive" {
		return model.GitSource{}, false
	}
	url, _, err := p.option("git.url")
	if err != nil || url == "" {
		return model.GitSource{}, false
	}
	tag, _, err := p.option("git.branch")
	if err != nil || tag == "" {
		return model.GitSource{}, false
	}
	return model.GitSource{URL: url, Ref: tag}, true
}
