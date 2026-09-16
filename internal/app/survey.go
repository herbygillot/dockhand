package app

import (
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"os"
	"path/filepath"
)

func surveyIndex(config Config, indexed bool) (portindex.Config, error) {
	index := portindex.Config{}
	if indexed {
		cache, err := os.UserCacheDir()
		if err != nil {
			return portindex.Config{}, err
		}
		index.CacheDirectory = filepath.Join(cache, "dockhand", "indexes")
		if config.MacPortsPrefix != "" {
			index.Executable = filepath.Join(config.MacPortsPrefix, "bin", "portindex")
		}
	}
	return index, nil
}
