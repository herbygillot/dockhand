package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/macports"
)

// NamedProgram is a program dockhand runs as the command line, its
// environment, or the configuration file named it: its path, and what
// named it, "--tool git", "GIT_BIN", or "[tools] git".
type NamedProgram struct {
	config.Program
	Path, Source string
}

// NamedPrograms are the programs named for this command line, by name,
// each checked as it's resolved: --tool, then its variable, then [tools]
// in file (read from configPath, which a refusal names), each refused
// where it names a program that isn't there, with no fallback to the
// search (the person's choice, 2026-11-03). look finds a bare name on
// PATH. A program nothing names isn't among them.
func (o Options) NamedPrograms(file config.File, configPath string, look func(string) (string, error)) (map[string]NamedProgram, error) {
	named := map[string]NamedProgram{}
	places := o.Places()
	for _, program := range config.Programs {
		value, source, label := o.Tools[program.Name], "--tool "+program.Name, "--tool "+program.Name
		if value == "" {
			value, source, label = o.Getenv(program.Variable), program.Variable, program.Variable
		}
		if value == "" {
			value, source, label = file.Tools.Get(program.Key), "[tools] "+program.Key, configPath+": tools."+program.Key
		}
		if value == "" {
			continue
		}
		path, err := programPath(value, look, places)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		named[program.Name] = NamedProgram{Program: program, Path: path, Source: source}
	}
	return named, nil
}

// programPath is the file a value names: one with a slash, its leading ~
// expanded and made absolute against the command line's directory, which
// must be an executable file; a bare name, the one look finds on PATH.
func programPath(value string, look func(string) (string, error), places config.Places) (string, error) {
	if !strings.Contains(value, "/") {
		path, err := look(value)
		if err != nil {
			return "", fmt.Errorf("%s isn't on PATH", value)
		}
		return path, nil
	}
	path, err := places.Absolute(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return "", fmt.Errorf("%s doesn't exist", value)
	case err != nil:
		return "", err
	case info.IsDir():
		return "", fmt.Errorf("%s is a directory", value)
	case info.Mode()&0o111 == 0:
		return "", fmt.Errorf("%s isn't executable", value)
	}
	return path, nil
}

// lookNamed is look with the named programs ahead of it: a named one is
// its path, portindex follows a named port-tclsh's directory, and every
// other name is look's.
func lookNamed(named map[string]NamedProgram, look func(string) (string, error)) func(string) (string, error) {
	if len(named) == 0 {
		return look
	}
	return func(name string) (string, error) {
		if program, ok := named[name]; ok {
			return program.Path, nil
		}
		if tclsh, ok := named[macports.TclShell]; ok && name == macports.PortIndexer {
			path := filepath.Join(filepath.Dir(tclsh.Path), macports.PortIndexer)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path, nil
			}
		}
		return look(name)
	}
}

// ParseTool reads one --tool value, name=path, refusing another shape or
// a name that isn't a program dockhand runs.
func ParseTool(value string) (name, path string, err error) {
	name, path, ok := strings.Cut(value, "=")
	if !ok || path == "" {
		return "", "", fmt.Errorf("--tool %s: expected name=path, for example --tool git=/opt/homebrew/bin/git", value)
	}
	for _, program := range config.Programs {
		if program.Name == name {
			return name, path, nil
		}
	}
	names := config.ProgramNames()
	return "", "", fmt.Errorf("--tool %s: %s isn't a program dockhand runs; the names are %s, and %s", value, name, strings.Join(names[:len(names)-1], ", "), names[len(names)-1])
}
