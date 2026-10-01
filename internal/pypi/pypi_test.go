package pypi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAReleasesFilesAreRead(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/flatbuffers/25.12.19/json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"info": {}, "urls": [{"filename": "flatbuffers-25.12.19-py2.py3-none-any.whl", "packagetype": "bdist_wheel"}]}`)
	}))
	t.Cleanup(server.Close)
	client := Client{HTTP: server.Client(), Base: server.URL}
	files, err := client.Files(t.Context(), "flatbuffers", "25.12.19")
	require.NoError(t, err)
	require.Equal(t, []File{{Filename: "flatbuffers-25.12.19-py2.py3-none-any.whl", PackageType: "bdist_wheel"}}, files)
	_, err = client.Files(t.Context(), "flatbuffers", "0.0")
	require.ErrorContains(t, err, "HTTP 404")
}

// A reply past the 8 MiB read of a release says it's too large, whether
// PyPI declared its length or not, where a cut body read as "unexpected
// EOF"; and a redirect from HTTPS to plain HTTP isn't followed.
func TestAReleaseTooLargeOrDowngradedIsSaid(t *testing.T) {
	t.Parallel()
	large := `{"urls": [], "info": {"description": "` + strings.Repeat("x", maxRelease) + `"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/declared/1.0/json" {
			w.Header().Set("Content-Length", fmt.Sprint(len(large)))
		}
		fmt.Fprint(w, large)
	}))
	t.Cleanup(server.Close)
	client := Client{HTTP: server.Client(), Base: server.URL}
	for _, project := range []string{"declared", "streamed"} {
		_, err := client.Files(t.Context(), project, "1.0")
		require.ErrorContains(t, err, "pypi: "+project+" 1.0: its JSON is larger than the 8 MiB dockhand reads of a release")
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"urls": []}`) }))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	_, err := Client{HTTP: secure.Client(), Base: secure.URL}.Files(t.Context(), "flatbuffers", "25.12.19")
	require.ErrorContains(t, err, "redirect downgraded HTTPS")
}

func TestASourceArchivesProjectIsReadFromItsURL(t *testing.T) {
	t.Parallel()
	project, ok := SourceProject("https://files.pythonhosted.org/packages/source/f/flatbuffers/flatbuffers-25.12.19.tar.gz")
	require.True(t, ok)
	require.Equal(t, "flatbuffers", project)
	for _, address := range []string{"https://github.com/google/flatbuffers/archive/v25.12.19.tar.gz", "https://files.pythonhosted.org/packages/ab/cd/flatbuffers-25.12.19.tar.gz", "::"} {
		_, ok := SourceProject(address)
		require.False(t, ok, address)
	}
}
