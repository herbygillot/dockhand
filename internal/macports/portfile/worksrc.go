package portfile

import (
	"regexp"
	"slices"
	"strings"
)

// worksrcPath is a literal path below ${worksrcpath}, as a Portfile's
// build names one: system -W ${worksrcpath}/pfff, or
// build.dir ${worksrcpath}/semgrep-core.
var worksrcPath = regexp.MustCompile(`\$\{?worksrcpath\}?/([A-Za-z0-9._+-]+(?:/[A-Za-z0-9._+-]+)*)`)

// WorksrcPaths are the literal paths below ${worksrcpath} a Portfile names
// outside comments, sorted, each once. A path built from a variable is
// read up to it, so ${worksrcpath}/src/${name} names src. A path the
// build makes, as a build directory, is named too, so a caller weighs
// each against what the source had before.
func WorksrcPaths(src []byte) []string {
	var paths []string
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, match := range worksrcPath.FindAllStringSubmatch(line, -1) {
			named := strings.TrimRight(match[1], "/.")
			if named != "" && !slices.Contains(paths, named) {
				paths = append(paths, named)
			}
		}
	}
	slices.Sort(paths)
	return paths
}
