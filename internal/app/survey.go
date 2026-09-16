package app

import (
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
)

// indexCacheDirectory is the PortIndex cache shared by discovery, dependents,
// and Tart staging. It is disposable and independent of the state database.
func indexCacheDirectory() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "dockhand", "indexes"), nil
}

func surveyIndex(config Config, indexed bool) (portindex.Config, error) {
	index := portindex.Config{}
	if indexed {
		directory, err := indexCacheDirectory()
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
