// Package forge holds the canonical list of code forges dockhand
// knows: their identity, their canonical domains, and the URL shapes
// their repositories take. A forge is either a hosted institution with
// a fixed domain (GitHub, Codeberg) or self-hostable software whose
// instances vary (Gitea, cgit); GitLab is both, carrying its canonical
// instance as the default. At binds a self-hosted family to a specific
// instance.
package forge

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Forge is one code forge.
type Forge struct {
	// Name is the forge's human identity.
	Name string
	// Domain is the canonical instance ("github.com"), possibly with a
	// path ("example.org/gitea"); empty for purely self-hosted families
	// until At binds one.
	Domain string
	// Tilde marks SourceHut's ~author path shape.
	Tilde bool
	// NoAuthor marks forges whose projects hang directly off the root
	// (cgit).
	NoAuthor bool
	// CloneSuffix is appended to repository URLs (".git" for cgit).
	CloneSuffix string
}

// The known forges.
var (
	GitHub    = &Forge{Name: "GitHub", Domain: "github.com"}
	GitLab    = &Forge{Name: "GitLab", Domain: "gitlab.com"}
	Gitea     = &Forge{Name: "Gitea"}
	Codeberg  = &Forge{Name: "Codeberg", Domain: "codeberg.org"}
	SourceHut = &Forge{Name: "SourceHut", Domain: "git.sr.ht", Tilde: true}
	Bitbucket = &Forge{Name: "Bitbucket", Domain: "bitbucket.org"}
	NotABug   = &Forge{Name: "NotABug", Domain: "notabug.org"}
	Cgit      = &Forge{Name: "cgit", NoAuthor: true, CloneSuffix: ".git"}

	// None is the absence of an identifiable forge: a plain git host
	// with the ordinary author/project shape, bindable with At like any
	// self-hosted family. Not in All — it is what a repository is on
	// when it is on none of them.
	None = &Forge{Name: "none"}
)

// All lists every known forge.
var All = []*Forge{GitHub, GitLab, Gitea, Codeberg, SourceHut, Bitbucket, NotABug, Cgit}

// ErrUnbound reports a LookupProject on a forge with no instance
// domain.
var ErrUnbound = errors.New("forge: no instance domain (bind one with At)")

// At returns a copy bound to a specific instance. The instance may be
// a bare domain, carry a path (gitea under a subpath), or be a full
// https URL; the scheme is normalized away.
func (f *Forge) At(instance string) *Forge {
	c := *f
	instance = strings.TrimPrefix(instance, "https://")
	instance = strings.TrimPrefix(instance, "http://")
	c.Domain = strings.TrimSuffix(instance, "/")
	return &c
}

// LookupProject returns the repository URL for a project on this
// forge — the address the rest of dockhand fetches tags from, reads
// trees at, or opens PRs against.
func (f *Forge) LookupProject(author, project string) (string, error) {
	if f.Domain == "" {
		return "", fmt.Errorf("%w: %s", ErrUnbound, f.Name)
	}
	if project == "" {
		return "", fmt.Errorf("forge: %s: no project named", f.Name)
	}
	path := project
	switch {
	case f.NoAuthor:
	case author == "":
		return "", fmt.Errorf("forge: %s: no author named", f.Name)
	case f.Tilde:
		path = "~" + author + "/" + project
	default:
		path = author + "/" + project
	}
	return "https://" + f.Domain + "/" + path + f.CloneSuffix, nil
}

// FromDomain identifies the known forge at a domain; None and false
// for unrecognized hosts.
func FromDomain(domain string) (*Forge, bool) {
	domain = strings.TrimSuffix(domain, "/")
	for _, f := range All {
		if f.Domain != "" && strings.EqualFold(domain, f.Domain) {
			return f, true
		}
	}
	return None, false
}

// FromRepoURL identifies the forge a repository URL belongs to; None
// and false for unrecognized hosts or unparseable URLs.
func FromRepoURL(repo string) (*Forge, bool) {
	u, err := url.Parse(repo)
	if err != nil || u.Host == "" {
		return None, false
	}
	return FromDomain(u.Host)
}
