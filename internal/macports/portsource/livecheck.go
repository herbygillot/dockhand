package portsource

import (
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
	for _, key := range macports.LivecheckOptions {
		if err := evaluated(port, key); err != nil {
			return Spec{}, ErrUnsupported.Because("%v", err)
		}
	}
	live := Livecheck{Type: port.Options["livecheck.type"], Regex: port.Options["livecheck.regex"], Version: port.Options["livecheck.version"]}
	sourceVersion := archiveSourceVersion(port)
	if standard, err := port.LivecheckStandard(); live.Type != "regex" && live.Type != "regexm" || live.Regex == "" || live.Version != sourceVersion || err != nil || !standard {
		return Spec{}, ErrUnsupported.Because("require standard regex livecheck for the evaluated port version, without custom hooks")
	}
	if err := readListing(port, &live); err != nil {
		return Spec{}, err
	}
	return Spec{CurrentVersion: port.Version, SourceVersion: sourceVersion, Catalog: HTTPRegex, Livecheck: live}, nil
}

// readListing reads how Base's curl fetch would read the livecheck URL: the
// URL itself, joined as Base joins it, and the curl options the listing
// discovery honors. Unsupported curl behavior is refused rather than
// silently changing the request.
func readListing(port macports.PortInfo, live *Livecheck) error {
	for _, key := range macports.LivecheckListingOptions {
		if err := evaluated(port, key); err != nil {
			return ErrUnsupported.Because("%v", err)
		}
	}
	live.Headers = map[string]string{}
	live.Multiline = live.Type == "regexm"
	// These two options are Tcl lists de-escaped by Base with join.
	parts, errs := syntax.ListValues(port.Options["livecheck.url"])
	if len(errs) != 0 {
		return ErrUnsupported.Because("invalid livecheck URL")
	}
	live.URL = strings.Join(parts, " ")
	parsed, err := url.Parse(live.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ErrUnsupported.Because("livecheck requires an HTTP(S) URL without credentials")
	}
	ignore, err := port.Bool("livecheck.ignore_sslcert")
	if err != nil || ignore {
		return ErrUnsupported.Because("livecheck.ignore_sslcert must be disabled")
	}
	if live.Compression, err = port.Bool("livecheck.compression"); err != nil {
		return ErrUnsupported.Because("%v", err)
	}
	options, errs := syntax.ListValues(port.Options["livecheck.curloptions"])
	if len(errs) != 0 {
		return ErrUnsupported.Because("malformed livecheck.curloptions")
	}
	for i := 0; i < len(options); i += 2 {
		if options[i] != "--append-http-header" || i+1 >= len(options) {
			return ErrUnsupported.Because("livecheck.curloptions option %q is not supported", options[i])
		}
		name, value, ok := strings.Cut(options[i+1], ":")
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if !ok || name != "Accept" && name != "User-Agent" || strings.ContainsAny(value, "\r\n") || live.Headers[name] != "" {
			return ErrUnsupported.Because("unsupported or repeated livecheck HTTP header %q", name)
		}
		live.Headers[name] = value
	}
	return nil
}
