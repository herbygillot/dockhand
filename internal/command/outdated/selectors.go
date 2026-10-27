package outdated

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// Selectors choose ports by MacPorts' index, on master as fetched: --mine,
// the ports whose maintainers line names the person, and --maintainer,
// --category, and --not-maintainer, as MacPorts combines them,
// alternatives within a flag and all of the flags together (the docs
// dig's I38a, at the person's "Dockhand should be able to accept port
// selectors"). outdated, update, and ship take them alike, and a
// selection is held, limited, and staged as the person's own ports are.
type Selectors struct {
	Mine                                    bool
	Maintainers, Categories, NotMaintainers []string
}

// Register adds the selector flags to a command; what says what it does
// with the ports chosen.
func (s *Selectors) Register(cmd *cobra.Command, what string) {
	cmd.Flags().BoolVar(&s.Mine, "mine", false, what+" the ports whose maintainers line names you (config maintainer)")
	cmd.Flags().StringSliceVar(&s.Maintainers, "maintainer", nil, what+" the ports a maintainer keeps, as the index names them: a GitHub login, an address, openmaintainer, or nomaintainer; repeat or comma-separate for any of several")
	cmd.Flags().StringSliceVar(&s.Categories, "category", nil, what+" the ports of a category, such as devel; repeat or comma-separate for any of several")
	cmd.Flags().StringSliceVar(&s.NotMaintainers, "not-maintainer", nil, "leave out the ports a maintainer keeps, beside --maintainer, --category, or --mine")
}

// Chosen reports whether any selector chose ports.
func (s Selectors) Chosen() bool {
	return s.Mine || len(s.Maintainers)+len(s.Categories)+len(s.NotMaintainers) > 0
}

// flags are the selection as its flags are written, for a command that
// says to run outdated over it again.
func (s Selectors) flags() string {
	var words []string
	if s.Mine {
		words = append(words, "--mine")
	}
	for _, flag := range []struct {
		name   string
		values []string
	}{{"--maintainer", s.Maintainers}, {"--category", s.Categories}, {"--not-maintainer", s.NotMaintainers}} {
		for _, value := range flag.values {
			words = append(words, flag.name, value)
		}
	}
	return strings.Join(words, " ")
}

// maintainers are the maintainers the selection asks for, --mine's with
// --maintainer's, each once.
func (s Selectors) maintainers(mine []string) []string {
	var maintainers []string
	if s.Mine {
		maintainers = append(maintainers, mine...)
	}
	for _, maintainer := range s.Maintainers {
		if !slices.Contains(maintainers, maintainer) {
			maintainers = append(maintainers, maintainer)
		}
	}
	return maintainers
}
