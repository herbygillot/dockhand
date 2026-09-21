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
	// Multiline is Base's regexm: the expression is matched once against
	// the whole listing rather than against each line.
	Multiline bool
	// Overridden says the maintainer replaced the forge PortGroup's default
	// livecheck, the catalog page, with their own definition of the latest
	// version: discovery runs that livecheck as Base would and proves its
	// answer against the catalog before it becomes a release.
	Overridden bool `json:",omitempty"`
	Type       string
	URL        string
	Regex      string
	Version    string
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

// Purpose says what an interpretation is for. The same PortGroup options are
// read either way; discovery additionally needs a livecheck convention that
// names a catalog to consult.
type Purpose int

const (
	// Edit accepts any port with an evaluated version. A port without a
	// recognized forge PortGroup is an archive source; a present but
	// malformed forge declaration is still an error.
	Edit Purpose = iota
	// Discovery requires a supported regex livecheck for the evaluated
	// version: a forge tags page or feed, or an HTTP listing for archive sources.
	Discovery
)

// Interpret reads the port's source convention for one purpose.
func Interpret(port macports.PortInfo, purpose Purpose) (Spec, error) {
	github := present(port, "github.author")
	gitlab := present(port, "gitlab.author")
	if !github && !gitlab {
		if purpose == Discovery {
			return discoverListing(port)
		}
		if port.Version == "" {
			return Spec{}, fmt.Errorf("%w: missing evaluated version", ErrUnsupported)
		}
		return Spec{CurrentVersion: port.Version, SourceVersion: archiveSourceVersion(port)}, nil
	}
	if github == gitlab {
		return Spec{}, fmt.Errorf("%w: require exactly one recognized source PortGroup", ErrUnsupported)
	}
	var spec Spec
	var err error
	if github {
		spec, err = interpret(port, GitHub, "github", "https://github.com")
	} else {
		spec, err = interpret(port, GitLab, "gitlab", port.Options["gitlab.instance"])
	}
	if err != nil || purpose == Edit {
		return spec, err
	}
	for _, key := range macports.LivecheckOptions {
		if err := evaluated(port, key); err != nil {
			return Spec{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
	}
	spec.Livecheck = Livecheck{
		Type: port.Options["livecheck.type"], URL: port.Options["livecheck.url"],
		Regex: port.Options["livecheck.regex"], Version: port.Options["livecheck.version"],
	}
	switch {
	case spec.Livecheck.Type == "none":
		return Spec{}, fmt.Errorf("%w: the port's livecheck is disabled (livecheck.type none); name the version to update to", ErrUnsupported)
	case spec.Livecheck.Type != "regex" && spec.Livecheck.Type != "regexm":
		return Spec{}, fmt.Errorf("%w: livecheck.type %s is not a regex livecheck; name the version to update to", ErrUnsupported, spec.Livecheck.Type)
	case spec.Livecheck.Regex == "" || (spec.Livecheck.Version != spec.CurrentVersion && spec.Livecheck.Version != spec.SourceVersion):
		return Spec{}, fmt.Errorf("%w: require a regex livecheck for the evaluated port version", ErrUnsupported)
	}
	if spec.Forge == GitHub {
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
	}
	// The PortGroup's default livecheck reads the catalog page; the catalog
	// itself is the same list without the page's truncation, so discovery
	// consults it directly. Any other livecheck is the maintainer's own
	// definition of the latest version, run as written and proven against
	// the catalog; it is read with the curl behavior Base would use.
	expected, err := spec.tagsURL()
	if err != nil {
		return Spec{}, err
	}
	if trimURL(spec.Livecheck.URL) == trimURL(expected) && spec.Livecheck.Type == "regex" {
		return spec, nil
	}
	if err := readListing(port, &spec.Livecheck); err != nil {
		return Spec{}, err
	}
	if port.Options["dockhand.livecheck_standard"] != "1" {
		return Spec{}, fmt.Errorf("%w: the port's livecheck has custom hooks; name the version to update to", ErrUnsupported)
	}
	spec.Livecheck.Overridden = true
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
	keys := macports.ForgeOptions(prefix)
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

// archiveSourceVersion is the spelling an archive source uses for the port's
// version: livecheck.version, which MacPorts compares upstream observations
// against, when the Portfile evaluates one, otherwise the port version. The
// perl5 PortGroup derives the port version from its module version and
// checks CPAN for the latter; for most ports the two are the same string.
func archiveSourceVersion(port macports.PortInfo) string {
	if value := port.Options["livecheck.version"]; value != "" && port.OptionErrors["livecheck.version"] == "" {
		return value
	}
	return port.Version
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
