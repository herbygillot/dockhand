package app

import (
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
)

// indexCacheDirectory is the PortIndex cache shared by discovery, dependents,
// and Tart staging. It is disposable and independent of the state database.
// Configuration, then DOCKHAND_INDEX_CACHE, then the user cache directory
// select it; tests point it at a temporary directory.
func indexCacheDirectory(config Config) (string, error) {
	for _, chosen := range []string{config.IndexCacheDirectory, os.Getenv("DOCKHAND_INDEX_CACHE")} {
		if chosen != "" {
			return filepath.Abs(chosen)
		}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "dockhand", "indexes"), nil
}

func surveyIndex(config Config, indexed bool) (portindex.Config, error) {
	index := portindex.Config{}
	if indexed {
		directory, err := indexCacheDirectory(config)
		if err != nil {
			return portindex.Config{}, err
		}
		index.CacheDirectory = directory
		if config.MacPortsPrefix != "" {
			index.Executable = filepath.Join(config.MacPortsPrefix, "bin", "portindex")
		}
	}
	if config.Tart.PortIndexExecutable != "" {
		index.Executable = config.Tart.PortIndexExecutable
	}
	return index, nil
}
