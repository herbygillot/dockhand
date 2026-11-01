package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A description's mentions are its logins after an "@", each once, a
// team's whole; an address, code, and an HTML comment, as MacPorts'
// template's guidance is, mention no one, as GitHub renders them.
func TestADescriptionsMentionsAreWhomItNotifies(t *testing.T) {
	t.Parallel()
	body := "<!-- Note: it is @nobody's job; see\n@template -->\n#### Description\n\nUpdate jq. Ping @ryandesign and (@jmroot), cc @macports/ports-team.\n" +
		"Mail ada@example.org, or `@incode`.\n```\n@fenced\n```\n~~~sh\n@tilde\n~~~\nThanks @RyanDesign, @a-b-c; not @-x or @" + "abcdefghijklmnopqrstuvwxyzabcdefghijklmn.\n<!-- unterminated @hidden"
	require.Equal(t, []string{"ryandesign", "jmroot", "macports/ports-team", "a-b-c"}, mentions(body))
	require.Empty(t, mentions(""))
}
