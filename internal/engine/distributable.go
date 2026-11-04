package engine

import (
	"context"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/distributable"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/model"
)

// distributableJudge asks MacPorts' checker whether a port is
// distributable (comparison.DistributableJudge): the port as its
// evaluation gives it, its license_noconflict from its text, and each
// dependency, transitively, from the base tree's port index, with the
// license_noconflict of those whose Portfiles name one read from their
// text. Where the base's index isn't cached, or the checker can't run,
// it's unknown, and nothing is said.
type distributableJudge struct{ e *Engine }

// closureLimit bounds the dependencies a judgment reads, a GTK port's
// being some hundreds.
const closureLimit = 5000

func (j distributableJudge) Distributable(ctx context.Context, base model.Source, port macports.PortInfo, text []byte) assess.Verdict {
	e := j.e
	cache, err := e.indexCache()
	if err != nil || base.Tree == "" {
		return assess.Verdict{}
	}
	exempt := e.noConflicts(ctx, string(base.Tree))
	infos := map[string]distributable.Info{}
	top := distributable.Info{License: port.Options["license"], InstallsLibs: true}
	for _, dependency := range port.Dependencies {
		if dependency.Phase == "build" || dependency.Phase == "lib" {
			top.Dependencies = append(top.Dependencies, dependency.Port)
		}
	}
	read := false
	found, err := portindex.ReadCached(ctx, cache, string(base.Tree), func(index *portindex.Index) error {
		read = true
		if text != nil {
			top.NoConflict = portfile.TopWords(text, "license_noconflict")
		} else if entry, err := index.Lookup(port.Name); err == nil {
			top.NoConflict = exempt[entry.Portdir]
		}
		infos[port.Name] = top
		queue := append([]string(nil), top.Dependencies...)
		for len(queue) > 0 && len(infos) < closureLimit {
			name := queue[0]
			queue = queue[1:]
			if _, seen := infos[name]; seen {
				continue
			}
			entry, err := index.Lookup(name)
			if err != nil {
				continue
			}
			info := distributable.Info{License: entry.Fields["license"], InstallsLibs: entry.Fields["installs_libs"] != "no", NoConflict: exempt[entry.Portdir]}
			for _, field := range []string{"depends_build", "depends_lib"} {
				for _, spec := range strings.Fields(entry.Fields[field]) {
					info.Dependencies = append(info.Dependencies, spec[strings.LastIndexByte(spec, ':')+1:])
				}
			}
			infos[name] = info
			queue = append(queue, info.Dependencies...)
		}
		return nil
	})
	if err != nil || !found || !read {
		return assess.Verdict{}
	}
	reader, err := e.selectionReader()
	if err != nil {
		return assess.Verdict{}
	}
	verdict, err := reader.Evaluator.Distributable(ctx, port.Name, infos)
	if err != nil {
		return assess.Verdict{}
	}
	return assess.Verdict{Known: verdict.Known, OK: verdict.OK, Reason: verdict.Reason}
}

// noConflicts are the license_noconflict words of each Portfile in tree
// that names one, by its directory, as its text writes them.
func (e *Engine) noConflicts(ctx context.Context, tree string) map[string][]string {
	paths, err := e.Repo.GrepTree(ctx, tree, `license_noconflict`, "*/*/Portfile")
	if err != nil || len(paths) == 0 {
		return nil
	}
	files, err := e.Repo.ReadFiles(ctx, tree, paths)
	if err != nil {
		return nil
	}
	exempt := map[string][]string{}
	for name, data := range files {
		if words := portfile.TopWords(data, "license_noconflict"); len(words) > 0 {
			exempt[path.Dir(name)] = words
		}
	}
	return exempt
}
