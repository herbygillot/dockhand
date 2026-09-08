package portnote

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The splitter is the contract every rule in this package and every
// caller of it rests on, and the one promise it makes is that Text is
// the file's own bytes. A rule may read the prose however it likes; a
// quote that was reflowed is not a quote, and a rule that asks what a
// comment sits against needs line numbers nothing else preserves.

func TestABlockKeepsItsOwnBytes(t *testing.T) {
	src := "name privoxy\n" +
		"subport ${name}-pki-bundle {\n" +
		"    # NOTE: Please rev-bump squirrel-ime\n" +
		"    #       whenever librime-devel updates\n" +
		"}\n"
	got := Blocks([]byte(src))
	require.Len(t, got, 1, "a comment inside a braced body is still a comment")
	assert.Equal(t,
		"    # NOTE: Please rev-bump squirrel-ime\n"+
			"    #       whenever librime-devel updates",
		got[0].Text, "verbatim: the marker and the indentation as the Portfile writes them")
	assert.Equal(t, "Please rev-bump squirrel-ime whenever librime-devel updates", got[0].Prose,
		"the prose is the wrapped sentence read as one sentence, with the NOTE: prefix off")
	assert.Equal(t, []string{"Please rev-bump squirrel-ime", "whenever librime-devel updates"}, got[0].Lines)
	assert.Equal(t, 2, got[0].Start)
	assert.Equal(t, 3, got[0].End)
}

// A block ends at the first line that is not a comment. That is what
// keeps a sentence in one stanza from being read together with a
// sentence in the next — and it is why a negation further down a file
// cannot silence an instruction above it.
func TestACodeLineEndsTheBlock(t *testing.T) {
	src := "# one\n# still one\nname ffmpeg\n# two\n"
	got := Blocks([]byte(src))
	require.Len(t, got, 2)
	assert.Equal(t, "# one\n# still one", got[0].Text)
	assert.Equal(t, "one still one", got[0].Prose)
	assert.Equal(t, "# two", got[1].Text)
	assert.Equal(t, 3, got[1].Start)
	assert.Equal(t, 3, got[1].End)
}

// A blank line is not a comment either, so two paragraphs of comment
// are two blocks — the shape a maintainer uses to say two unrelated
// things at the top of a file.
func TestABlankLineEndsTheBlock(t *testing.T) {
	got := Blocks([]byte("# one\n\n# two\n"))
	require.Len(t, got, 2)
	assert.Equal(t, "# one", got[0].Text)
	assert.Equal(t, "# two", got[1].Text)
}

// A file that ends inside a comment still closes it. The last line has
// no successor to flush it, and a Portfile whose final line is a
// comment is common enough that losing it would lose real annotations.
func TestAFileEndingInACommentClosesIt(t *testing.T) {
	got := Blocks([]byte("name ffmpeg\n# the last word"))
	require.Len(t, got, 1)
	assert.Equal(t, "# the last word", got[0].Text)
	assert.Equal(t, 1, got[0].Start)
	assert.Equal(t, 1, got[0].End)
}

func TestAFileWithNoCommentsHasNoBlocks(t *testing.T) {
	assert.Empty(t, Blocks([]byte("name ffmpeg\nversion 7.1\n")))
	assert.Empty(t, Blocks(nil))
}
