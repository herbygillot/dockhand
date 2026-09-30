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
