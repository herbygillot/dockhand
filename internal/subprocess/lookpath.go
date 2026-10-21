package subprocess

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LookPath is where name runs from in env: name itself where it holds a
// slash, else the first executable file of that name in env's PATH, as
// exec.LookPath finds one in the process's.
func LookPath(name string, env []string) (string, error) {
	if strings.Contains(name, "/") {
		if executable(name) {
			return name, nil
		}
		return "", fmt.Errorf("subprocess: %s is not an executable file", name)
	}
	path := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			path = value
		}
	}
	for _, directory := range filepath.SplitList(path) {
		if directory == "" {
			continue
		}
		if candidate := filepath.Join(directory, name); executable(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("subprocess: %s isn't on the PATH given", name)
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
