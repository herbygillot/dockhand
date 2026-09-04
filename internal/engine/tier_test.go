package engine

// Reading a port's maintainer tier off a Portfile.
//
// Two layers, checked separately. The scan and the reduction are pure
// functions over text and are held as tables, which is where the
// keyword traps live; the read itself needs a repository, because what
// it is actually claiming is that the bytes come off the BRANCH and not
// off the host's checkout, and only a real commit can say so.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
)

// The tier is read off the BRANCH and not off the ports tree: the
// change edited that Portfile, so its own commit carries the bytes the
// pull request is about. Reading the host's checked-out copy would
// answer a question nobody asked — the tier a reviewer will see is the
// one in the diff.
func TestThePortTierIsReadFromTheBranchsOwnPortfile(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	gittest.Commit(t, repo, "dockhand/jq-1.8", primary, "sysutils/jq/Portfile",
		"name jq\nmaintainers @herby openmaintainer\nversion 1.8\n", "jq: update to 1.8")
	n := record.Record{Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq"}}}

	assert.Equal(t, render.TierOpenmaintainer, portTier(ctx, repo, "dockhand/jq-1.8", &n))

	// An absolute portdir is the shape a record actually carries — the
	// directory on the host — and only its last two elements are the
	// tree-relative path git needs.
	abs := record.Record{Subjects: []record.Subject{{Port: "jq", Portdir: repo.Root + "/sysutils/jq"}}}
	assert.Equal(t, render.TierOpenmaintainer, portTier(ctx, repo, "dockhand/jq-1.8", &abs))
}

// Every failure is TierUnknown and none of them is reported. A tier is
// a courtesy on top of a pull request's age, and a status pass that
// failed because a Portfile moved would spend the reader's whole report
// on the least important thing in it.
func TestAnUnreadableTierIsSilentlyUnknown(t *testing.T) {
	ctx := context.Background()
	repo, _ := engineRepo(t)
	n := record.Record{Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq"}}}

	assert.Equal(t, render.TierUnknown, portTier(ctx, repo, "dockhand/jq-1.8", &n),
		"the fixture Portfile names no maintainers")
	assert.Equal(t, render.TierUnknown, portTier(ctx, repo, "dockhand/no-such-branch", &n))
	assert.Equal(t, render.TierUnknown, portTier(ctx, repo, "dockhand/jq-1.8", nil))
	assert.Equal(t, render.TierUnknown, portTier(ctx, repo, "dockhand/jq-1.8",
		&record.Record{Subjects: []record.Subject{{Port: "jq"}}}), "no portdir, no answer")
}

func TestTierOfReducesTheMaintainersField(t *testing.T) {
	for _, c := range []struct {
		name  string
		field string
		want  render.Tier
	}{
		{"nobody at all", "nomaintainer", render.TierNomaintainer},
		{"a person and the nomaintainer keyword", "devans nomaintainer", render.TierNomaintainer},
		{"open to strangers", "@herby openmaintainer", render.TierOpenmaintainer},
		{"open, keyword first", "openmaintainer @herby", render.TierOpenmaintainer},
		{"open, shouted", "@herby OpenMaintainer", render.TierOpenmaintainer},
		{"one maintainer", "@herby", render.TierMaintained},
		{"an obfuscated address", "gmail.com:herby.gillot", render.TierMaintained},
		{"a braced sublist", "{nicos @NicosPavlov}", render.TierMaintained},
		{"a braced sublist, open", "{nicos @NicosPavlov} openmaintainer", render.TierOpenmaintainer},
		{"no field", "", render.TierUnknown},
		{"a field naming nobody", "{}", render.TierUnknown},
	} {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, tierOf(c.field))
		})
	}
}

// nomaintainer wins over a named person: a port can list somebody and
// still declare that nobody is on the hook, and the keyword is the
// declaration that settles it.
func TestNomaintainerOutranksANamedMaintainer(t *testing.T) {
	assert.Equal(t, render.TierNomaintainer, tierOf("@herby nomaintainer openmaintainer"))
}

// A missing maintainers field and a field saying "nobody" are different
// claims, and only one of them was made. Reading silence as
// nomaintainer would put a follow-up in front of the project saying a
// port has no maintainer on the strength of a line that is not there.
func TestSilenceIsNotNomaintainer(t *testing.T) {
	assert.Equal(t, render.TierUnknown, tierOf(maintainersField("version 1.7\nrevision 0\n")))
}

func TestMaintainersFieldIsFoundWhereItIsWritten(t *testing.T) {
	for _, c := range []struct {
		name     string
		portfile string
		want     string
	}{
		{"a plain line", "name jq\nmaintainers @herby\nversion 1.7\n", "@herby"},
		{"indented", "name jq\n    maintainers    @herby\n", "@herby"},
		{"tab separated", "maintainers\t@herby openmaintainer\n", "@herby openmaintainer"},
		{"continued", "maintainers @herby \\\n            @other\nversion 1.7\n", "@herby @other"},
		{"absent", "name jq\nversion 1.7\n", ""},
		// The trap this field invites: three of the tokens a Portfile can
		// carry end in the same eleven letters, so nothing here may match
		// a prefix or a substring.
		{"nomaintainer is not the field", "nomaintainer\nversion 1.7\n", ""},
		{"maintainers_ is not the field", "maintainers_note @herby\n", ""},
		{"a word that merely contains it", "# see maintainers above\nversion 1.7\n", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, maintainersField(c.portfile))
		})
	}
}
