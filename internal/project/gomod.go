package project

import (
	"path"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// GoModule is what a go.mod declares: its module's path, the Go release
// its go directive requires, and the modules it requires.
type GoModule struct {
	Path string
	// Go is the go directive as it's written: "1.24", "1.24.0", or
	// "1.24.2"; empty where there's none. The toolchain directive isn't
	// kept: Go documents it as a suggestion.
	Go       string
	Requires []GoRequire
}

// GoRequire is one module a go.mod requires, and whether it's required
// only for another's sake, marked indirect.
type GoRequire struct {
	Path, Version string
	Indirect      bool
}

// ReadGoMod reads a go.mod as the go command does, refusing a statement it
// doesn't know.
func ReadGoMod(data []byte) (GoModule, error) {
	parsed, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return GoModule{}, err
	}
	return goModule(parsed), nil
}

// ReadGoModLax reads a go.mod passing over the statements it doesn't know,
// as a later Go's may be, for what it requires.
func ReadGoModLax(data []byte) (GoModule, error) {
	parsed, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil {
		return GoModule{}, err
	}
	return goModule(parsed), nil
}

func goModule(parsed *modfile.File) GoModule {
	var found GoModule
	if parsed.Module != nil {
		found.Path = parsed.Module.Mod.Path
	}
	if parsed.Go != nil {
		found.Go = parsed.Go.Version
	}
	for _, required := range parsed.Require {
		found.Requires = append(found.Requires, GoRequire{Path: required.Mod.Path, Version: required.Mod.Version, Indirect: required.Indirect})
	}
	return found
}

// Binary is the program `go build` makes at the module's root: named for
// its path's last element, less a major version's /vN suffix, as Go names
// it. False for a go.mod without a module path, which makes none that can
// be named.
func (m GoModule) Binary() (string, bool) {
	if m.Path == "" {
		return "", false
	}
	prefix, _, ok := module.SplitPathVersion(m.Path)
	if !ok {
		prefix = m.Path
	}
	return path.Base(prefix), true
}
