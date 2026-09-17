package dependency

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInstalledHelpers(t *testing.T) {
	t.Parallel()
	if os.Getenv("DOCKHAND_TEST_DEPENDENCY_HELPERS") != "1" {
		t.Skip("set DOCKHAND_TEST_DEPENDENCY_HELPERS=1 to exercise installed tools and an upstream Go archive")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	tools := Tools{Go2Port: os.Getenv("GO2PORT_BIN"), Cargo2Port: os.Getenv("CARGO2PORT_BIN")}
	t.Run("cargo2port", func(t *testing.T) {
		executable, err := tools.Resolve(Cargo)
		require.NoError(t, err)
		sum := strings.Repeat("a", 64)
		lock := fmt.Sprintf("version = 4\n[[package]]\nname = \"fixture\"\nversion = \"1.0.0\"\n[[package]]\nname = \"serde\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = %q\n", sum)
		result, err := Generate(ctx, Cargo, executable, Input{Archive: sourceArchive(t, map[string]string{"root/Cargo.lock": lock}), Worksrcdir: "root"})
		require.NoError(t, err)
		require.Equal(t, []string{"serde", "1.0.0", sum}, result.Values[Cargo])
	})
	t.Run("go2port", func(t *testing.T) {
		executable, err := tools.Resolve(Go)
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://codeload.github.com/google/go-querystring/tar.gz/refs/tags/v1.2.0", nil)
		require.NoError(t, err)
		response, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)
		file, err := os.Create(filepath.Join(t.TempDir(), "source.tar.gz"))
		require.NoError(t, err)
		_, err = io.Copy(file, io.LimitReader(response.Body, 16<<20))
		require.NoError(t, err)
		require.NoError(t, file.Close())
		result, err := Generate(ctx, Go, executable, Input{Archive: file.Name(), Worksrcdir: "go-querystring-1.2.0", Package: "github.com/google/go-querystring", Tag: "v1.2.0"})
		require.NoError(t, err)
		require.Contains(t, result.Values[Go], "github.com/google/go-cmp")
		require.Contains(t, result.Values[Go], "v0.6.0")
	})
}
