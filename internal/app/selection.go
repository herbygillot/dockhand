package app

import (
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/selection"
)

// portReader resolves names against a staged index. mirror, when given, lets
// a cold cache bootstrap from the mirror's index; the commands that stay
// offline pass nil. The stager remembers what it staged, so one command
// resolving names repeatedly against the same tree indexes it once, and it
// indexes without the base: name lookup needs no generation of master.
func portReader(config Config, repo *git.Repository, mirror *portindex.Mirror) (*selection.Reader, error) {
	native := &eval.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	index, err := surveyIndex(config, true)
	if err != nil {
		return nil, err
	}
	index.Mirror = mirror
	return &selection.Reader{Evaluator: native, Index: &portindex.Stager{Repo: repo, Config: index, NativePlatform: native.NativePlatform, WithoutBase: true}}, nil
}
