package macports

import (
	"regexp"
	"strings"
)

// CIWorkflow is where MacPorts' CI workflow lives in the ports tree.
const CIWorkflow = ".github/workflows/main.yml"

// ciMatrix is the workflow's matrix of runners, os: [macos-14, ...].
var ciMatrix = regexp.MustCompile(`(?m)^\s*os:\s*\[([^\]]*)\]`)

// CIReleases are the macOS releases MacPorts' CI builds every pull request
// on, by product version, "14", "15", "26", as its workflow's matrix names
// its runners (macos-14 and so on); none where the workflow names none.
func CIReleases(workflow string) []string {
	match := ciMatrix.FindStringSubmatch(workflow)
	if match == nil {
		return nil
	}
	var releases []string
	for _, runner := range strings.Split(match[1], ",") {
		runner = strings.Trim(strings.TrimSpace(runner), `"'`)
		if release, ok := strings.CutPrefix(runner, "macos-"); ok && release != "" && strings.Trim(release, "0123456789") == "" {
			releases = append(releases, release)
		}
	}
	return releases
}
