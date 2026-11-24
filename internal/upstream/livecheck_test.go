package upstream

import (
	"context"
	"net/http"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/stretchr/testify/require"
)

type livecheckVersions struct{}

type livecheckDocuments struct {
	Catalog
	page []byte
}

func (d livecheckDocuments) Document(context.Context, string, http.Header) ([]byte, bool, error) {
	return d.page, true, nil
}

func (livecheckVersions) ExtractVersions(_ context.Context, expression, page string, multiline bool) ([]string, error) {
	if expression != `v([0-9.]+)` || page != "v1.16.2 v1.16.3-rc1" || multiline {
		return nil, errUnexpectedLivecheckInput
	}
	return []string{"1.16.2", "1.16.3-rc1"}, nil
}

func (livecheckVersions) SelectVersion(context.Context, string, string, []macports.VersionCandidate) (macports.VersionSelection, error) {
	panic("not used")
}

func (livecheckVersions) CompareVersions(context.Context, string, string) (int, error) {
	panic("not used")
}

var errUnexpectedLivecheckInput = &unexpectedLivecheckInput{}

type unexpectedLivecheckInput struct{}

func (*unexpectedLivecheckInput) Error() string { return "unexpected livecheck input" }

func TestLivecheckReadsAndFiltersOneListing(t *testing.T) {
	t.Parallel()
	pageText := "v1.16.2 v1.16.3-rc1"
	documents := livecheckDocuments{page: []byte(pageText)}
	service := &Service{Catalogs: map[portsource.Forge]Catalog{portsource.GitHub: documents}, Versions: livecheckVersions{}}
	spec := portsource.Spec{Forge: portsource.GitHub, SourceVersion: "1.16.0", Livecheck: portsource.Livecheck{URL: "https://example.test/versions", Regex: `v([0-9.]+)`}}
	page, headers, candidates, from, passed, err := service.livecheck(t.Context(), macports.PortInfo{Name: "terraform-1.16", Version: "1.16.0"}, spec)
	require.NoError(t, err)
	require.Equal(t, pageText, string(page))
	require.Empty(t, headers)
	require.Equal(t, []macports.VersionCandidate{{Version: "1.16.2", MatchText: "1.16.2"}}, candidates)
	require.Empty(t, from)
	require.Equal(t, "1.16.3-rc1", passed)
}
