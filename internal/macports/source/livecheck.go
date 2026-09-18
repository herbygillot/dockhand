package source

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// HTTPRegex describes an evaluated non-forge release listing. Unsupported curl
// behavior is rejected rather than silently changing the Portfile's request.
const HTTPRegex Catalog = "http-regex"

func discoverListing(port macports.PortInfo) (Spec, error) {
	keys := []string{"livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version", "livecheck.ignore_sslcert", "livecheck.compression", "livecheck.curloptions", "dockhand.livecheck_standard"}
	for _, key := range keys {
		if err := evaluated(port, key); err != nil {
			return Spec{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
	}
	live := Livecheck{Type: port.Options["livecheck.type"], Regex: port.Options["livecheck.regex"], Version: port.Options["livecheck.version"], Headers: map[string]string{}}
	// These two options are Tcl lists de-escaped by Base with join.
	parts, errs := syntax.ListValues(port.Options["livecheck.url"])
	if len(errs) != 0 {
		return Spec{}, fmt.Errorf("%w: invalid livecheck URL", ErrUnsupported)
	}
	live.URL = strings.Join(parts, " ")
	parsed, err := url.Parse(live.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Spec{}, fmt.Errorf("%w: livecheck requires an HTTP(S) URL without credentials", ErrUnsupported)
	}
	sourceVersion := archiveSourceVersion(port)
	if live.Type != "regex" && live.Type != "regexm" || live.Regex == "" || live.Version != sourceVersion || port.Options["dockhand.livecheck_standard"] != "1" {
		return Spec{}, fmt.Errorf("%w: require standard regex livecheck for the evaluated port version, without custom hooks", ErrUnsupported)
	}
	live.Multiline = live.Type == "regexm"
	ignore, err := boolean(port.Options["livecheck.ignore_sslcert"])
	if err != nil || ignore {
		return Spec{}, fmt.Errorf("%w: livecheck.ignore_sslcert must be disabled", ErrUnsupported)
	}
	live.Compression, err = boolean(port.Options["livecheck.compression"])
	if err != nil {
		return Spec{}, err
	}
	options, errs := syntax.ListValues(port.Options["livecheck.curloptions"])
	if len(errs) != 0 {
		return Spec{}, fmt.Errorf("%w: malformed livecheck.curloptions", ErrUnsupported)
	}
	for i := 0; i < len(options); i += 2 {
		if options[i] != "--append-http-header" || i+1 >= len(options) {
			return Spec{}, fmt.Errorf("%w: livecheck.curloptions option %q is not supported", ErrUnsupported, options[i])
		}
		name, value, ok := strings.Cut(options[i+1], ":")
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if !ok || name != "Accept" && name != "User-Agent" || strings.ContainsAny(value, "\r\n") || live.Headers[name] != "" {
			return Spec{}, fmt.Errorf("%w: unsupported or repeated livecheck HTTP header %q", ErrUnsupported, name)
		}
		live.Headers[name] = value
	}
	return Spec{CurrentVersion: port.Version, SourceVersion: sourceVersion, Catalog: HTTPRegex, Livecheck: live}, nil
}

func boolean(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "1", "yes", "true", "on":
		return true, nil
	case "0", "no", "false", "off":
		return false, nil
	}
	return false, fmt.Errorf("%w: unsupported livecheck boolean", ErrUnsupported)
}
