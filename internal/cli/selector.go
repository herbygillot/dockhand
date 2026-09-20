package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
)

// A target token is normally a port name, or a snapshot-relative path such as
// devel/bashunit/Portfile, which every verb passes down unchanged. A token
// that names a place on the filesystem instead -- ".", "./bashunit", "..",
// an absolute path -- is a third thing: it means "the port I am standing in",
// which only this edge can answer, because nothing below it knows the working
// directory. It is resolved here, once, to the port's name, the one spelling
// both snapshot resolution and record selection accept.
//
// Only an explicitly relative or absolute token is read as a path. A bare
// "devel/bashunit" stays snapshot-relative, as it has always been: the two
// readings would otherwise differ for anyone running from inside a ports tree.
func pathToken(token string) bool {
	return token == "." || token == ".." ||
		strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") ||
		filepath.IsAbs(token)
}

// portName resolves a filesystem token to the name of the port whose
// directory it names, and returns any other token unchanged.
func portName(token string) (string, error) {
	if !pathToken(token) {
		return token, nil
	}
	directory := filepath.Clean(token)
	if filepath.Base(directory) == "Portfile" {
		directory = filepath.Dir(directory)
	}
	info, err := os.Stat(filepath.Join(directory, "Portfile"))
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("%s holds no Portfile; name a port, a port directory, or a Portfile", token)
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	name := filepath.Base(absolute)
	if !macports.ValidName(name) {
		return "", fmt.Errorf("%s is not a port directory; name a port, a port directory, or a Portfile", token)
	}
	return name, nil
}

// portNames resolves each token of a multi-port selection.
func portNames(tokens []string) ([]string, error) {
	resolved := make([]string, 0, len(tokens))
	for _, token := range tokens {
		name, err := portName(token)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, name)
	}
	return resolved, nil
}
