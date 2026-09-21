package tart

import (
	"encoding/json"
	"fmt"
	"github.com/herbygillot/dockhand/internal/verify"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
)

// SourceIndex returns the index recipe frozen in an accepted Tart configuration,
// staged through the shared cache root. An empty cache root falls back to the
// legacy location beneath the recorded artifact directory.
func SourceIndex(build record.BuildConfig, indexCache string, mirror *portindex.Mirror) (portindex.Config, error) {
	var config Config
	if build.Provider != verify.ProviderTart || len(build.ProviderConfig) == 0 {
		return portindex.Config{}, fmt.Errorf("tart: recorded configuration required for source indexing")
	}
	if err := json.Unmarshal(build.ProviderConfig, &config); err != nil {
		return portindex.Config{}, err
	}
	if config.Platform != build.Platform || config.PortIndexExecutable == "" || config.PortIndexDigest == "" || config.ArtifactDirectory == "" {
		return portindex.Config{}, fmt.Errorf("tart: incomplete recorded index configuration")
	}
	return sourceIndex(config, indexCache, mirror), nil
}

// sourceIndex is the staging index recipe; mirror lets a cold cache bootstrap
// from the mirror's index, since verification already reaches the network.
func sourceIndex(c Config, indexCache string, mirror *portindex.Mirror) portindex.Config {
	if indexCache == "" {
		indexCache = filepath.Join(c.ArtifactDirectory, "indexes")
	}
	return portindex.Config{Executable: c.PortIndexExecutable, Digest: c.PortIndexDigest, CacheDirectory: indexCache, Mirror: mirror}
}
