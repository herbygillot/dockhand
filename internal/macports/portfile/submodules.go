package portfile

import (
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// submoduleUpdate is the one command a Git-fetched port's submodule hook
// runs: git submodule update --init, with --recursive or a --depth, which
// checks out exactly what the fetched commit pins.
var submoduleUpdate = regexp.MustCompile(`^git submodule update --init( --recursive)?( --depth [0-9]+)?$`)

// SubmoduleHook reports whether a hook body only checks out the Git
// submodules the fetched commit pins: each command is system -W
// ${worksrcpath} "git submodule update --init…", the standard hook a
// Git-fetched port runs after its fetch or its extract, as fetchguard
// reads a post-fetch and a source move reads either.
func SubmoduleHook(body string) bool {
	src := []byte(body)
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return false
	}
	commands := 0
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		commands++
		if len(cmd.Words) != 4 || cmd.Words[0].Span.Text(src) != "system" || cmd.Words[1].Span.Text(src) != "-W" {
			return false
		}
		dir := cmd.Words[2].Span.Text(src)
		if dir != "${worksrcpath}" && dir != "$worksrcpath" {
			return false
		}
		text := strings.Trim(cmd.Words[3].Span.Text(src), `"{}`)
		if !submoduleUpdate.MatchString(text) {
			return false
		}
	}
	return commands > 0
}
