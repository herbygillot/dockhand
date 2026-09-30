package portedit

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/pypi"
)

// A PyPI release that publishes only wheels says so where its source
// archive can't be had, rather than that it isn't published yet: PyPI
// never gets one for it (the py-flatbuffers addition's finding 2).
func TestAPyPIReleaseWithOnlyWheelsSaysSo(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flatbuffers/25.12.19/json":
			fmt.Fprint(w, `{"urls": [{"filename": "flatbuffers-25.12.19-py2.py3-none-any.whl", "packagetype": "bdist_wheel"}]}`)
		case "/demo/1.0/json":
			fmt.Fprint(w, `{"urls": [{"filename": "demo-1.0.tar.gz", "packagetype": "sdist"}, {"filename": "demo-1.0-py3-none-any.whl", "packagetype": "bdist_wheel"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	index := pypi.Client{HTTP: server.Client(), Base: server.URL}
	missing := fmt.Errorf("archives: downloading flatbuffers-25.12.19.tar.gz: %w", &fetch.StatusError{Status: http.StatusNotFound, URL: "https://files.pythonhosted.org/packages/source/f/flatbuffers/flatbuffers-25.12.19.tar.gz"})
	urls := []string{"https://files.pythonhosted.org/packages/source/f/flatbuffers/flatbuffers-25.12.19.tar.gz"}

	err := wheelsOnly(t.Context(), index, "flatbuffers-25.12.19.tar.gz", urls, "25.12.19", missing)
	var status *fetch.StatusError
	require.ErrorAs(t, err, &status, "the refusal stays reachable")
	require.EqualError(t, err, "archives: downloading flatbuffers-25.12.19.tar.gz: fetch: HTTP 404 for https://files.pythonhosted.org/packages/source/f/flatbuffers/flatbuffers-25.12.19.tar.gz; PyPI publishes only wheels for flatbuffers 25.12.19, and a release without a source archive never gets one, so the port needs its source from elsewhere, such as the project's own release on its forge")

	require.Equal(t, missing, wheelsOnly(t.Context(), index, "flatbuffers-25.12.19.tar.gz", []string{"https://files.pythonhosted.org/packages/source/d/demo/demo-1.0.tar.gz"}, "1.0", missing), "a release with a source archive leaves the error as it is")
	require.Equal(t, missing, wheelsOnly(t.Context(), index, "flatbuffers-25.12.19.tar.gz", urls, "0.0", missing), "PyPI knowing nothing leaves it too")
	require.Equal(t, missing, wheelsOnly(t.Context(), index, "flatbuffers-25.12.19.tar.gz", []string{"https://github.com/google/flatbuffers/archive/v25.12.19.tar.gz"}, "25.12.19", missing), "a URL of another host isn't PyPI's")
	refused := fmt.Errorf("archives: %w", &fetch.StatusError{Status: http.StatusForbidden})
	require.Equal(t, refused, wheelsOnly(t.Context(), index, "flatbuffers-25.12.19.tar.gz", urls, "25.12.19", refused), "only a 404 asks")
	require.Equal(t, errors.ErrUnsupported, wheelsOnly(t.Context(), index, "flatbuffers-25.12.19.tar.gz", urls, "25.12.19", errors.ErrUnsupported))
}
