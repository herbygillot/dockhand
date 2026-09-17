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

type regexExtractor interface {
	ExtractVersions(context.Context, string, string) ([]string, error)
}

func (s *Service) discoverListing(ctx context.Context, port macports.PortInfo, spec portsource.Spec) (result Result, err error) {
	result = Result{CurrentVersion: port.Version, Assessment: Unknown, ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	defer func() {
		if err != nil {
			result.Detail = err.Error()
		}
	}()
	native, ok := s.Versions.(regexExtractor)
	if !ok {
		return result, fmt.Errorf("upstream: native livecheck extraction is unavailable")
	}
	if !automatic(port.Version) {
		return result, fmt.Errorf("%w: require a stable or prerelease numeric version", errAutomaticUnsupported)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.Livecheck.URL, nil)
	if err != nil {
		return result, err
	}
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
	versions, err := native.ExtractVersions(ctx, spec.Livecheck.Regex, string(page))
	if err != nil {
		return result, err
	}
	var candidates []macports.VersionCandidate
	for _, version := range versions {
		if admits(port.Version, version) {
			candidates = append(candidates, macports.VersionCandidate{Version: version, MatchText: version})
		}
	}
	index, comparison, err := s.newest(ctx, port.Version, `^(.*)$`, candidates, "livecheck", "a version")
	if err != nil {
		return result, err
	}
	version := candidates[index].Version
	if comparison > 0 {
		evaluated, err := s.EvaluateVersion(ctx, version)
		if err != nil {
			return result, err
		}
		if evaluated != version {
			return result, fmt.Errorf("%w: livecheck capture is not the evaluated version", errAutomaticUnsupported)
		}
	}
	digest := sha256.Sum256(page)
	release := record.Release{Selection: record.Selection{CurrentVersion: port.Version, NoUpdate: comparison <= 0}, Archive: true, Version: version, ObservedAt: result.ObservedAt, Listing: &record.ReleaseListing{URL: spec.Livecheck.URL, ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified"), SHA256: hex.EncodeToString(digest[:])}}
	result.finish(release, port.Version, "Selected "+version+" from livecheck", "")
	result.Evidence = []Observation{{Source: string(portsource.HTTPRegex), Version: version, URL: spec.Livecheck.URL, ObservedAt: result.ObservedAt}}
	return result, nil
}
