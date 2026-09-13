package upstream

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/tcl/syntax"
)

func (s *Service) githubSource(port macports.PortInfo) (forge.Repository, TagPattern, error) {
	for _, key := range []string{"github.author", "github.project", "github.version", "git.branch"} {
		if port.OptionErrors[key] != "" {
			return nil, TagPattern{}, fmt.Errorf("upstream: cannot evaluate %s", key)
		}
	}
	repository := port.Options["github.author"] + "/" + port.Options["github.project"]
	if port.Options["github.version"] != port.Version {
		return nil, TagPattern{}, fmt.Errorf("upstream: bumps currently require a GitHub PortGroup version matching the evaluated port version")
	}
	values := []string{}
	for _, key := range []string{"github.tag_prefix", "github.tag_suffix"} {
		value, ok := port.Options[key]
		if !ok || port.OptionErrors[key] != "" {
			return nil, TagPattern{}, ErrTagPattern
		}
		parts, errs := syntax.ListValues(value)
		if len(errs) > 0 {
			return nil, TagPattern{}, ErrTagPattern
		}
		values = append(values, strings.Join(parts, " "))
	}
	pattern := TagPattern{Prefix: values[0], Suffix: values[1]}
	if port.Options["git.branch"] != pattern.tag(port.Version) {
		return nil, TagPattern{}, ErrTagPattern
	}
	if s == nil || s.Repositories == nil {
		return nil, TagPattern{}, fmt.Errorf("upstream: repository reader is required")
	}
	bound, err := s.Repositories.Repository(repository)
	if err != nil {
		return nil, TagPattern{}, err
	}
	if bound == nil || bound.Name() != repository {
		return nil, TagPattern{}, fmt.Errorf("upstream: repository reader returned a different source")
	}
	return bound, pattern, nil
}

// githubLivecheck recognizes the supported PortGroup/livecheck convention.
// Repository URLs come from the bound forge adapter, not version-selection code.
func githubLivecheck(port macports.PortInfo, repository forge.Repository) (string, error) {
	for _, key := range []string{"github.tarball_from", "livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version"} {
		if port.OptionErrors[key] != "" {
			return "", fmt.Errorf("%w: cannot evaluate %s", ErrAutomaticUnsupported, key)
		}
	}
	if port.Options["livecheck.type"] != "regex" || strings.TrimRight(port.Options["livecheck.url"], "/") != repository.TagsPageURL() || port.Options["livecheck.regex"] == "" || port.Options["livecheck.version"] != port.Version || !stableVersion.MatchString(port.Version) {
		return "", fmt.Errorf("%w: require a stable numeric version and a matching GitHub tags livecheck", ErrAutomaticUnsupported)
	}
	mode := port.Options["github.tarball_from"]
	if mode == "" {
		mode = "archive"
	}
	if mode != "releases" && mode != "archive" && mode != "tarball" {
		return "", fmt.Errorf("%w: unknown GitHub archive mode", ErrAutomaticUnsupported)
	}
	return mode, nil
}
