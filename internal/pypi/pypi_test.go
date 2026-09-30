package pypi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
