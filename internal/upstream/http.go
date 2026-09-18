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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.Livecheck.URL, nil)
	if err != nil {
		return result, err
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	for key, value := range spec.Livecheck.Headers {
		request.Header.Set(key, value)
	}
	if !spec.Livecheck.Compression {
		request.Header.Set("Accept-Encoding", "identity")
	}
	// A bounded observation must fail as incomplete, never yield a truncated
	// candidate set. This is a listing limit, not a forge catalog pagination cap.
	response, err := fetch.Open(s.HTTP, request, 16<<20)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	page, err := io.ReadAll(response.Body)
	if err != nil {
		return result, err
	}
	if !utf8.Valid(page) {
		return result, fmt.Errorf("upstream: livecheck listing is not UTF-8")
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
	release := record.Release{Selection: record.Selection{CurrentVersion: port.Version, NoUpdate: comparison <= 0}, Archive: true, Version: evaluated, ObservedAt: result.ObservedAt, Listing: &record.ReleaseListing{URL: spec.Livecheck.URL, ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified"), SHA256: hex.EncodeToString(digest[:])}}
	if version != evaluated {
		release.SourceVersion = version
	}
	result.finish(release, port.Version, "Selected "+version+" from livecheck", "")
	result.Evidence = []Observation{{Source: string(portsource.HTTPRegex), Version: version, URL: spec.Livecheck.URL, ObservedAt: result.ObservedAt}}
	return result, nil
}
