package upstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/macports"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
)

func (s *Service) discoverListing(ctx context.Context, port macports.PortInfo, spec portsource.Spec) (result Result, err error) {
	result = Result{CurrentVersion: port.Version, Assessment: Unknown, ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	defer func() {
		if err != nil {
			result.Detail = err.Error()
		}
	}()
	// The listing is compared against the source's own spelling of the
	// version, as Base compares it against livecheck.version; the port
	// version is evaluated from the selected spelling afterwards.
	current := spec.SourceVersion
	if !automatic(current) {
		return result, fmt.Errorf("%w: require a stable or prerelease numeric version", errAutomaticUnsupported)
	}
	page, validators, err := s.listing(ctx, port, spec)
	if err != nil {
		return result, err
	}
	versions, err := s.Versions.ExtractVersions(ctx, spec.Livecheck.Regex, string(page), spec.Livecheck.Multiline)
	if err != nil {
		return result, err
	}
	var candidates []macports.VersionCandidate
	for _, version := range versions {
		if admits(current, version) {
			candidates = append(candidates, macports.VersionCandidate{Version: version, MatchText: version})
		}
	}
	index, comparison, err := s.newest(ctx, current, `^(.*)$`, candidates, "livecheck", "a version")
	if err != nil {
		return result, err
	}
	version := candidates[index].Version
	// The release records the evaluated port version; the source spelling
	// rides beside it only when the Portfile derives one from the other.
	evaluated := version
	if comparison > 0 {
		evaluated, err = s.EvaluateVersion(ctx, version)
		if err != nil {
			return result, err
		}
		if evaluated == "" || evaluated == port.Version {
			return result, fmt.Errorf("%w: livecheck capture does not change the evaluated version", errAutomaticUnsupported)
		}
	} else if current != port.Version {
		// Already current: the port version stands, and the derived
		// spelling is not evaluated for a release that will not be prepared.
		evaluated = port.Version
	}
	digest := sha256.Sum256(page)
	release := record.Release{Selection: record.Selection{CurrentVersion: port.Version, NoUpdate: comparison <= 0}, Archive: true, Version: evaluated, ObservedAt: result.ObservedAt, Listing: &record.ReleaseListing{URL: spec.Livecheck.URL, ETag: validators.Get("ETag"), LastModified: validators.Get("Last-Modified"), SHA256: hex.EncodeToString(digest[:])}}
	if version != evaluated {
		release.SourceVersion = version
	}
	result.finish(release, port.Version, "Selected "+version+" from livecheck", "")
	result.Evidence = []Observation{{Source: string(portsource.HTTPRegex), Version: version, URL: spec.Livecheck.URL, ObservedAt: result.ObservedAt}}
	return result, nil
}

// Documents is a catalog that fetches a livecheck document from the forge it
// serves through its own client, so the user's credentials and the forge's
// rate-limit handling apply, sending the given request headers in place of
// the client's own. served is false for a URL the forge does not serve, and
// the plain fetch is used instead.
type documents interface {
	Document(ctx context.Context, url string, headers http.Header) (body []byte, served bool, err error)
}

// requestHeaders are the headers Base's curl fetch sends for a livecheck:
// its user agent, Accept */*, the Portfile's curl options, and no compression
// when the Portfile turns it off. The user agent names MacPorts and libcurl
// as Base's does, run by dockhand: GitHub lays its JSON out by user agent,
// and only an agent carrying both tokens gets the indented form the
// maintainers' expressions are written against; an SDK's agent, or
// dockhand's alone, gets the compact form and the expressions match nothing.
func requestHeaders(port macports.PortInfo, spec portsource.Spec) http.Header {
	headers := http.Header{}
	agent := fetch.UserAgent
	if base := port.Options["dockhand.base_version"]; base != "" && port.OptionErrors["dockhand.base_version"] == "" {
		agent = "MacPorts/" + base + " libcurl " + fetch.UserAgent
	}
	headers.Set("User-Agent", agent)
	headers.Set("Accept", "*/*")
	for key, value := range spec.Livecheck.Headers {
		headers.Set(key, value)
	}
	if !spec.Livecheck.Compression {
		headers.Set("Accept-Encoding", "identity")
	}
	return headers
}

// listing reads the livecheck URL as Base's curl fetch would: through the
// forge's client when it serves the URL, otherwise plainly, with Base's
// request headers either way. The validators are the response's ETag and
// Last-Modified when the plain fetch read them.
func (s *Service) listing(ctx context.Context, port macports.PortInfo, spec portsource.Spec) ([]byte, http.Header, error) {
	headers := requestHeaders(port, spec)
	if documents, ok := s.Catalogs[spec.Forge].(documents); ok {
		body, served, err := documents.Document(ctx, spec.Livecheck.URL, headers)
		if served {
			if err != nil {
				return nil, nil, err
			}
			if !utf8.Valid(body) {
				return nil, nil, fmt.Errorf("upstream: livecheck listing is not UTF-8")
			}
			return body, http.Header{}, nil
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.Livecheck.URL, nil)
	if err != nil {
		return nil, nil, err
	}
	for key, values := range headers {
		request.Header[key] = values
	}
	// A bounded observation must fail as incomplete, never yield a truncated
	// candidate set. This is a listing limit, not a forge catalog pagination cap.
	response, err := fetch.Open(s.HTTP, request, 16<<20)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	page, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, err
	}
	if !utf8.Valid(page) {
		return nil, nil, fmt.Errorf("upstream: livecheck listing is not UTF-8")
	}
	return page, response.Header, nil
}
