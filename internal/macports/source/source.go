package source

import (
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"net/url"
	"strings"
	"unicode"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

var ErrUnsupported = errors.New("macports source: unsupported convention")
var ErrTagPattern = errors.New("macports source: tag convention is unknown")

type Forge string

const (
	GitHub Forge = forge.GitHub
	GitLab Forge = forge.GitLab
)

type Catalog string

const (
	Tags     Catalog = "tags"
	Releases Catalog = "releases"
)

// TagPattern is the version leaf's mapping between Portfile versions and tags.
type TagPattern = version.TagPattern

type Livecheck struct {
	Headers     map[string]string
	Compression bool
	Type        string
	URL         string
	Regex       string
	Version     string
}

type Spec struct {
	Forge          Forge
	Instance       string
	Repository     string
	CurrentVersion string
	SourceVersion  string
	Pattern        TagPattern
	Catalog        Catalog
	Livecheck      Livecheck
}

func Interpret(port macports.PortInfo) (Spec, error) {
	github := present(port, "github.author")
	gitlab := present(port, "gitlab.author")
	if github == gitlab {
		return Spec{}, fmt.Errorf("%w: require exactly one recognized source PortGroup", ErrUnsupported)
	}
	if github {
		return interpret(port, GitHub, "github", "https://github.com")
	}
	return interpret(port, GitLab, "gitlab", port.Options["gitlab.instance"])
}

func Discover(port macports.PortInfo) (Spec, error) {
	if !present(port, "github.author") && !present(port, "gitlab.author") {
		return discoverListing(port)
	}
	spec, err := Interpret(port)
	if err != nil {
		return Spec{}, err
	}
	for _, key := range []string{"livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version"} {
		if err := evaluated(port, key); err != nil {
			return Spec{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
	}
	spec.Livecheck = Livecheck{
		Type: port.Options["livecheck.type"], URL: port.Options["livecheck.url"],
		Regex: port.Options["livecheck.regex"], Version: port.Options["livecheck.version"],
	}
	if spec.Livecheck.Type != "regex" || spec.Livecheck.Regex == "" || (spec.Livecheck.Version != spec.CurrentVersion && spec.Livecheck.Version != spec.SourceVersion) {
		return Spec{}, fmt.Errorf("%w: require a regex livecheck for the evaluated port version", ErrUnsupported)
	}
	switch spec.Forge {
	case GitHub:
		if err := evaluated(port, "github.tarball_from"); err != nil {
			return Spec{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
		mode := port.Options["github.tarball_from"]
		if mode == "" {
			mode = "archive"
		}
		if mode != "releases" && mode != "archive" && mode != "tarball" {
			return Spec{}, fmt.Errorf("%w: unknown GitHub archive mode", ErrUnsupported)
		}
		if mode == "releases" {
			spec.Catalog = Releases
		}
		expected, err := spec.tagsURL()
		if err != nil {
			return Spec{}, err
		}
		if trimURL(spec.Livecheck.URL) != trimURL(expected) {
			return Spec{}, fmt.Errorf("%w: livecheck does not inspect the GitHub tags page", ErrUnsupported)
		}
	case GitLab:
		expected, err := spec.tagsURL()
		if err != nil {
			return Spec{}, err
		}
		if trimURL(spec.Livecheck.URL) != trimURL(expected) {
			return Spec{}, fmt.Errorf("%w: livecheck does not inspect the GitLab tags feed", ErrUnsupported)
		}
	}
	return spec, nil
}

func (s Spec) MatchText(tag string) (string, error) {
	web, err := s.webURL()
	if err != nil {
		return "", err
	}
	switch s.Forge {
	case GitHub:
		return appendPath(web, "archive", "refs", "tags", tag+".tar.gz")
	case GitLab:
		value, err := appendPath(web, "-", "tags", tag)
		return value + "</id>", err
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupported, s.Forge)
	}
}

func (s Spec) EvidenceURL(tag string) (string, error) {
	web, err := s.webURL()
	if err != nil {
		return "", err
	}
	if s.Forge == GitLab {
		return appendPath(web, "-", "tags", tag)
	}
	return appendPath(web, "archive", "refs", "tags", tag+".tar.gz")
}

func interpret(port macports.PortInfo, forge Forge, prefix, instance string) (Spec, error) {
	keys := []string{prefix + ".author", prefix + ".project", prefix + ".version", prefix + ".tag_prefix", prefix + ".tag_suffix", "git.branch"}
	if forge == GitLab {
		keys = append(keys, "gitlab.instance")
	}
	for _, key := range keys {
		if err := evaluated(port, key); err != nil {
			return Spec{}, err
		}
	}
	raw := port.Options[prefix+".version"]
	if raw == "" || port.Version == "" {
		return Spec{}, fmt.Errorf("%w: empty source or port version", ErrUnsupported)
	}
	values := make([]string, 0, 2)
	for _, key := range []string{prefix + ".tag_prefix", prefix + ".tag_suffix"} {
		parts, errs := syntax.ListValues(port.Options[key])
		if len(errs) != 0 {
			return Spec{}, ErrTagPattern
		}
		values = append(values, strings.Join(parts, " "))
	}
	pattern := TagPattern{Prefix: values[0], Suffix: values[1]}
	if port.Options["git.branch"] != pattern.Tag(raw) {
		return Spec{}, ErrTagPattern
	}
	instance, err := normalizeInstance(instance)
	if err != nil {
		return Spec{}, err
	}
	repository := port.Options[prefix+".author"] + "/" + port.Options[prefix+".project"]
	segments := 2
	if forge == GitLab {
		segments = 0
	}
	if !validPath(repository, segments) {
		return Spec{}, fmt.Errorf("macports source: invalid %s repository %q", prefix, repository)
	}
	return Spec{Forge: forge, Instance: instance, Repository: repository, CurrentVersion: port.Version, SourceVersion: raw, Pattern: pattern, Catalog: Tags}, nil
}

func evaluated(port macports.PortInfo, key string) error {
	if failure := port.OptionErrors[key]; failure != "" {
		return fmt.Errorf("macports source: cannot evaluate %s", key)
	}
	if _, ok := port.Options[key]; !ok {
		return fmt.Errorf("macports source: missing %s", key)
	}
	return nil
}

func present(port macports.PortInfo, key string) bool {
	_, value := port.Options[key]
	_, failure := port.OptionErrors[key]
	return value || failure
}

func normalizeInstance(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("macports source: invalid forge instance %q", value)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func validPath(value string, exact int) bool {
	parts := strings.Split(value, "/")
	if len(parts) < 2 || exact > 0 && len(parts) != exact {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.IndexFunc(part, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("?#%\\", r)
		}) >= 0 {
			return false
		}
	}
	return true
}

func (s Spec) webURL() (string, error) {
	return appendPath(s.Instance, strings.Split(s.Repository, "/")...)
}
func (s Spec) tagsURL() (string, error) {
	web, err := s.webURL()
	if err != nil {
		return "", err
	}
	if s.Forge == GitLab {
		value, err := appendPath(web, "-", "tags")
		return value + "?format=atom", err
	}
	return appendPath(web, "tags")
}
func appendPath(base string, elements ...string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.Join(elements, "/")
	parsed.RawPath = ""
	return parsed.String(), nil
}
func trimURL(value string) string { return strings.TrimRight(value, "/") }

// ForEditing accepts an explicit evaluated version without inventing a forge or
// tag identity. A present but malformed forge declaration remains an error.
func ForEditing(port macports.PortInfo) (Spec, error) {
	if !present(port, "github.author") && !present(port, "gitlab.author") {
		if port.Version == "" {
			return Spec{}, fmt.Errorf("%w: missing evaluated version", ErrUnsupported)
		}
		return Spec{CurrentVersion: port.Version, SourceVersion: port.Version}, nil
	}
	return Interpret(port)
}
