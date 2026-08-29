package upstream

import (
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/upstream/forge"
)

// Repo is a resolved upstream repository: which forge it is on, its
// URL, and the port's own declared tag scheme — prefixes and suffixes
// are read from options, never guessed.
type Repo struct {
	Forge     *forge.Forge
	URL       string
	TagPrefix string
	TagSuffix string
}

// coordSpec is how one carrier family names its coordinates: the
// option namespace, the forge, and the option binding a self-hosted
// instance when the family has one.
type coordSpec struct {
	ns       string
	f        *forge.Forge
	instance string
}

// coordSpecs covers every carrier family that is a git forge in its
// own right. The values mirror each PortGroup's own options and
// defaults.
var coordSpecs = map[portstyle.Type]coordSpec{
	// go.package parses into go.domain/author/project, so a go.setup
	// port is forge-resolvable at whatever git host its module names;
	// its tag scheme is delegated (see goTagNamespaces).
	portstyle.GoSetup:        {ns: "go", instance: "go.domain"},
	portstyle.GithubSetup:    {ns: "github", f: forge.GitHub},
	portstyle.GitlabSetup:    {ns: "gitlab", f: forge.GitLab, instance: "gitlab.instance"},
	portstyle.GiteaSetup:     {ns: "gitea", f: forge.Gitea, instance: "gitea.domain"},
	portstyle.CodebergSetup:  {ns: "codeberg", f: forge.Codeberg},
	portstyle.SourcehutSetup: {ns: "sourcehut", f: forge.SourceHut, instance: "sourcehut.instance"},
	portstyle.BitbucketSetup: {ns: "bitbucket", f: forge.Bitbucket},
	portstyle.NotabugSetup:   {ns: "notabug", f: forge.NotABug},
	portstyle.CgitSetup:      {ns: "cgit", f: forge.Cgit, instance: "cgit.url"},
}

// specsByNS indexes the specs for delegation lookups.
var specsByNS = func() map[string]coordSpec {
	out := make(map[string]coordSpec, len(coordSpecs))
	for _, s := range coordSpecs {
		out[s.ns] = s
	}
	return out
}()

// delegatedFamilies: setups that call another family's setup underneath
// — the coordinates land in whichever family's options the delegation
// set. Order matters only cosmetically; a port delegates to exactly
// one.
var delegatedFamilies = map[portstyle.Type][]string{
	portstyle.OctaveSetup: {"github", "gitlab", "bitbucket"},
	portstyle.RSetup:      {"github", "gitlab"},
}

// goTagNamespaces: go.setup keeps its own coordinates (go.domain and
// friends) but passes the tag scheme through to the family its domain
// selects, so the scheme lives in that family's options. A port setting
// go.tag_prefix directly still wins.
var goTagNamespaces = []string{"go", "github", "gitlab", "bitbucket", "sourcehut", "codeberg", "gitea"}

// specOptions returns the option names one spec's coordinates need.
func specOptions(spec coordSpec) []string {
	names := []string{spec.ns + ".project", spec.ns + ".tag_prefix", spec.ns + ".tag_suffix"}
	if spec.f == nil || !spec.f.NoAuthor {
		names = append(names, spec.ns+".author")
	}
	if spec.instance != "" {
		names = append(names, spec.instance)
	}
	return names
}

// CoordOptions returns the option names Coords needs for a carrier
// style; nil when the style has no git forge.
func CoordOptions(style portstyle.Type) []string {
	if families, ok := delegatedFamilies[style]; ok {
		var names []string
		for _, ns := range families {
			names = append(names, specOptions(specsByNS[ns])...)
		}
		return names
	}
	spec, ok := coordSpecs[style]
	if !ok {
		return nil
	}
	names := specOptions(spec)
	if style == portstyle.GoSetup {
		for _, ns := range goTagNamespaces {
			names = append(names, ns+".tag_prefix", ns+".tag_suffix")
		}
	}
	return names
}

// Coords derives the upstream repository from a carrier style and its
// evaluated options; false when the style has no git forge or the
// coordinates are incomplete.
func Coords(style portstyle.Type, opts map[string]string) (Repo, bool) {
	if families, ok := delegatedFamilies[style]; ok {
		for _, ns := range families {
			if r, ok := build(specsByNS[ns], opts); ok {
				return r, true
			}
		}
		return Repo{}, false
	}
	spec, ok := coordSpecs[style]
	if !ok {
		return Repo{}, false
	}
	r, ok := build(spec, opts)
	if !ok {
		return Repo{}, false
	}
	if style == portstyle.GoSetup {
		for _, ns := range goTagNamespaces {
			p, pok := opts[ns+".tag_prefix"]
			s, sok := opts[ns+".tag_suffix"]
			if pok || sok {
				r.TagPrefix, r.TagSuffix = p, s
				break
			}
		}
	}
	return r, true
}

// build assembles one spec's repository from the options.
func build(spec coordSpec, opts map[string]string) (Repo, bool) {
	f := spec.f
	if spec.instance != "" {
		instance := opts[spec.instance]
		if instance == "" {
			return Repo{}, false
		}
		if f == nil {
			// go.setup: the domain names whichever forge it names —
			// forge.None, with its plain git shape, when it names none.
			f, _ = forge.FromDomain(instance)
		}
		f = f.At(instance)
	}
	if f == nil {
		return Repo{}, false
	}
	url, err := f.LookupProject(opts[spec.ns+".author"], opts[spec.ns+".project"])
	if err != nil {
		return Repo{}, false
	}
	return Repo{
		Forge:     f,
		URL:       url,
		TagPrefix: opts[spec.ns+".tag_prefix"],
		TagSuffix: opts[spec.ns+".tag_suffix"],
	}, true
}
