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
	selected, err := s.Versions.SelectVersion(ctx, port.Version, `^(.*)$`, candidates)
	if err != nil {
		return result, err
	}
	if len(selected.Indices) == 0 {
		return result, fmt.Errorf("%w: no eligible stable version matches livecheck", ErrReleaseMissing)
	}
	if len(selected.Indices) != 1 {
		return result, fmt.Errorf("%w: multiple livecheck versions compare equal; specify a version explicitly", ErrReleaseAmbiguous)
	}
	index := selected.Indices[0]
	if index < 0 || index >= len(candidates) || selected.Comparison < -1 || selected.Comparison > 1 {
		return result, fmt.Errorf("upstream: invalid version selection")
	}
	version := candidates[index].Version
	if selected.Comparison > 0 {
		evaluated, err := s.EvaluateVersion(ctx, version)
		if err != nil {
			return result, err
		}
		if evaluated != version {
			return result, fmt.Errorf("%w: livecheck capture is not the evaluated version", errAutomaticUnsupported)
		}
	}
	digest := sha256.Sum256(page)
	result.CandidateVersion = version
	release := classified(record.Release{Archive: true, Version: version, CurrentVersion: port.Version, ObservedAt: result.ObservedAt, NoUpdate: selected.Comparison <= 0, Listing: &record.ReleaseListing{URL: spec.Livecheck.URL, ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified"), SHA256: hex.EncodeToString(digest[:])}}, port.Version)
	result.Release = &release
	result.Evidence = []Observation{{Source: string(portsource.HTTPRegex), Version: version, URL: spec.Livecheck.URL, ObservedAt: result.ObservedAt}}
	result.Assessment = UpdateAvailable
	result.Detail = "Selected " + version + " from livecheck"
	if result.Release.NoUpdate {
		result.Assessment = Current
		result.Detail = fmt.Sprintf("Already current at %s; latest eligible version is %s", port.Version, version)
	}
	return result, nil
}
