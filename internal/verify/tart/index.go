package tart

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
)

// SourceIndex returns the index recipe frozen in an accepted Tart configuration.
func SourceIndex(build record.BuildConfig) (portindex.Config, error) {
	var config Config
	if build.Provider != ProviderName || len(build.ProviderConfig) == 0 {
		return portindex.Config{}, fmt.Errorf("tart: recorded configuration required for source indexing")
	}
	if err := json.Unmarshal(build.ProviderConfig, &config); err != nil {
		return portindex.Config{}, err
	}
	if config.Platform != build.Platform || config.PortIndexExecutable == "" || config.PortIndexDigest == "" || config.ArtifactDirectory == "" {
		return portindex.Config{}, fmt.Errorf("tart: incomplete recorded index configuration")
	}
	return portindex.Config{Executable: config.PortIndexExecutable, Digest: config.PortIndexDigest, MirrorURL: config.PortIndexURL, CacheDirectory: filepath.Join(config.ArtifactDirectory, "indexes")}, nil
}
