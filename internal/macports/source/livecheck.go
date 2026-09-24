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
	for _, key := range macports.LivecheckOptions {
		if err := evaluated(port, key); err != nil {
			return Spec{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
	}
	live := Livecheck{Type: port.Options["livecheck.type"], Regex: port.Options["livecheck.regex"], Version: port.Options["livecheck.version"]}
	sourceVersion := archiveSourceVersion(port)
	if live.Type != "regex" && live.Type != "regexm" || live.Regex == "" || live.Version != sourceVersion || port.Options["dockhand.livecheck_standard"] != "1" {
		return Spec{}, fmt.Errorf("%w: require standard regex livecheck for the evaluated port version, without custom hooks", ErrUnsupported)
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
			return fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
	}
	live.Headers = map[string]string{}
	live.Multiline = live.Type == "regexm"
	// These two options are Tcl lists de-escaped by Base with join.
	parts, errs := syntax.ListValues(port.Options["livecheck.url"])
	if len(errs) != 0 {
		return fmt.Errorf("%w: invalid livecheck URL", ErrUnsupported)
	}
	live.URL = strings.Join(parts, " ")
	parsed, err := url.Parse(live.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: livecheck requires an HTTP(S) URL without credentials", ErrUnsupported)
	}
	ignore, err := port.Bool("livecheck.ignore_sslcert")
	if err != nil || ignore {
		return fmt.Errorf("%w: livecheck.ignore_sslcert must be disabled", ErrUnsupported)
	}
	if live.Compression, err = port.Bool("livecheck.compression"); err != nil {
		return fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	options, errs := syntax.ListValues(port.Options["livecheck.curloptions"])
	if len(errs) != 0 {
		return fmt.Errorf("%w: malformed livecheck.curloptions", ErrUnsupported)
	}
	for i := 0; i < len(options); i += 2 {
		if options[i] != "--append-http-header" || i+1 >= len(options) {
			return fmt.Errorf("%w: livecheck.curloptions option %q is not supported", ErrUnsupported, options[i])
		}
		name, value, ok := strings.Cut(options[i+1], ":")
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if !ok || name != "Accept" && name != "User-Agent" || strings.ContainsAny(value, "\r\n") || live.Headers[name] != "" {
			return fmt.Errorf("%w: unsupported or repeated livecheck HTTP header %q", ErrUnsupported, name)
		}
		live.Headers[name] = value
	}
	return nil
}
