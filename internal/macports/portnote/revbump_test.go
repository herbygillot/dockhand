package portnote

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The revbump-instruction family, against the comments the tree
// actually writes.
//
// Every source below is transcribed from a real Portfile, with the port
// it was read from named in the row. That is not decoration: a plan's
// own sentence for this rule — "increase the revision of the following
// ports when updating" — matches ZERO Portfiles, and a reader written
// to a fixture invented alongside it would have passed its tests
// forever while finding nothing in the field.
//
// The rows that must produce NOTHING are the more important half. A
// comment that says a revbump is unnecessary matches the family and
// means the opposite of it, and a caller downstream turns what comes
// out of here into a question that holds an unattended publication
// until a person answers it.
//
// The rows that produce an Instruction naming nobody are the other
// half of the point, and there are two different ones. Collective with
// no names is the class form — a criterion a reader must be shown and a
// tool must not act on. Neither collective nor named is a sentence
// about the port's own revision, and it is the caller with the port's
// name in hand that can tell the two from a roster; this package
// reports the shape and leaves that to it.

func TestTheInstructionFamilyAsTheTreeWritesIt(t *testing.T) {
	rows := []struct {
		name       string
		src        string
		known      []string
		found      bool
		ports      []string
		collective bool
	}{
		{
			name: "dav1d: a named list wrapped across two comment lines",
			src: "# Please increase the revision of libheif, ffmpeg and ffmpeg-devel whenever\n" +
				"# dav1d's version is updated.\n",
			found: true,
			ports: []string{"libheif", "ffmpeg", "ffmpeg-devel"},
		},
		{
			name:  "ffmpeg: one name, one line",
			src:   "# Please increase the revision of mpv whenever ffmpeg's version is updated.\n",
			found: true,
			ports: []string{"mpv"},
		},
		{
			name:  "curl: the condition names the port back",
			src:   "# Increase the revision of p5-www-curl whenever the version of curl gets updated.\n",
			found: true,
			ports: []string{"p5-www-curl"},
		},
		{
			name:  "db48: 'any time' ends the roster",
			src:   "# Increase the revision of p5-berkeleydb any time the db48 version changes.\n",
			found: true,
			ports: []string{"p5-berkeleydb"},
		},
		{
			name:  "grpc: a quoted name under a NOTE: prefix",
			src:   "# NOTE: Also rev-bump 'apache-arrow' when updating this port\n",
			found: true,
			ports: []string{"apache-arrow"},
		},
		{
			name:  "librime: rev-bump with the name straight after it",
			src:   "# Please rev-bump squirrel-ime whenever librime-devel updates\n",
			found: true,
			ports: []string{"squirrel-ime"},
		},
		{
			name: "sbcl: category prefixes, and 'possibly' inside the list",
			src: "# Please bump the revisions of math/maxima, math/fricas and possibly\n" +
				"# math/maxima-devel when this port changes.\n",
			found: true,
			ports: []string{"maxima", "fricas", "maxima-devel"},
		},
		{
			name: "openssl3: a header that points at bullets, with the caveats kept out of the names",
			src: "# Please revbump these ports when updating the openssl3 version/revision\n" +
				"#  - freeradius (#43461)\n" +
				"#  - openssh (#54990)\n" +
				"#  - p5-net-ssleay (#67321, for minor version bumps)\n" +
				"#  - openssl (to rebuild the shim links).\n",
			found: true,
			ports: []string{"freeradius", "openssh", "p5-net-ssleay", "openssl"},
		},
		{
			name:       "spdlog: a class in the header and a single bullet under it",
			src:        "# Ports that depend on this port must be revbump after update:\n# - tiledb\n",
			found:      true,
			ports:      []string{"tiledb"},
			collective: true,
		},
		{
			name: "protobuf3-cpp: the condition is on the line ABOVE the verb",
			src: "# NOTE: For a minor or major version number change, also\n" +
				"# NOTE:   Revbump et, protobuf-c, mosh and py-onnx\n",
			found: true,
			ports: []string{"et", "protobuf-c", "mosh", "py-onnx"},
		},
		{
			name: "icu: the unnamed form names nobody",
			src: "# Please increase the revision number of the dependents whenever the library\n" +
				"# version number changes.\n",
			found:      true,
			collective: true,
		},
		{
			name: "cmark: 'all ports that link with the library' is a class, not a roster",
			src: "# Any version update requires revbumping all ports that link with the library\n" +
				"# because the full version number is in the library's install name.\n",
			found:      true,
			collective: true,
		},
		{
			name:       "abseil: ports that depend on this port",
			src:        "# Ports that depend on this port must be revbump after update.\n",
			found:      true,
			collective: true,
		},
		{
			name: "geos: a conditional instruction about all dependents",
			src: "# NOTE: When updating this port, check whether the dylib name and/or version\n" +
				"# NOTE: changes. If so, all dependents will need to be rev-bumped.\n",
			found:      true,
			collective: true,
		},

		// The negations. Each of these matches the family and means the
		// opposite of it, and each is a comment block in the tree today.
		{
			name: "openssl3 line 147: too obscure to justify revbumping the dependents",
			src:  "# The ABI difference is real but is too obscure to justify\n# revbumping the dependents.\n",
		},
		{
			name: "py-sip4: no rev-bumps are needed",
			src:  "#  -> SO: no rev-bumps are be needed.\n",
		},
		{
			name: "perl5: rather not revbump many p5 ports",
			src:  "# Rather not revbump many p5 ports, so just fix it for new versions\n",
		},

		// Neither a roster nor a class. Both of these live on a
		// DEPENDENT and say what triggers a bump of it, and both are
		// reported here as what they are: privoxy names nobody at all,
		// and mpv names exactly the port whose file it sits in. Only a
		// caller that knows which port it is reading can tell either from
		// an instruction.
		{
			name:  "privoxy: a bump verb with no object at all",
			src:   "# Please increase the revision whenever curl-ca-bundle contents change\n",
			found: true,
		},
		{
			name:  "mpv: the comment names only the port it is written on",
			src:   "# Please revbump mpv whenever linked ffmpeg is updated! (See ffmpeg's Portfile)\n",
			found: true,
			ports: []string{"mpv"},
		},

		// An ordinary comment, which is what 41630 Portfiles carry.
		{
			name: "an ordinary comment says nothing about revisions",
			src:  "# This port needs a C99 compiler; see the upstream README.\n",
		},

		// The caller's roster overrides the word list: a token the
		// vocabulary would have stopped on is a port when the tree
		// already calls it one.
		{
			name:  "a known port whose name is an English word is still a port",
			src:   "# Please revbump when, whenever libwidget's version is updated.\n",
			known: []string{"when"},
			found: true,
			ports: []string{"when"},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got := Instructions([]byte(row.src), row.known)
			if !row.found {
				assert.Empty(t, got, "this comment is not an instruction")
				assert.False(t, MentionsRevbump([]byte(row.src)),
					"the cheap half must agree with the expensive one: a comment Instructions skips is one it says no about")
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, strings.TrimRight(row.src, "\n"), got[0].Quote,
				"the quote is the whole comment block, byte for byte")
			assert.Equal(t, row.ports, got[0].Ports)
			assert.Equal(t, row.collective, got[0].Collective)
			assert.True(t, MentionsRevbump([]byte(row.src)),
				"the cheap half must agree with the expensive one")
		})
	}
}

// Two instructions in one Portfile are two Instructions, because they
// are two sentences a person weighs separately — and a block ends at
// the first line that is not a comment, so a negation further down the
// file cannot silence an instruction above it.
func TestTwoCommentBlocksAreTwoInstructions(t *testing.T) {
	src := "# Please revbump mpv whenever ffmpeg's version is updated.\n" +
		"name ffmpeg\n" +
		"# Also rev-bump apache-arrow when updating this port\n" +
		"version 7.1\n"
	got := Instructions([]byte(src), nil)
	require.Len(t, got, 2)
	assert.Equal(t, "# Please revbump mpv whenever ffmpeg's version is updated.", got[0].Quote)
	assert.Equal(t, []string{"mpv"}, got[0].Ports)
	assert.Equal(t, "# Also rev-bump apache-arrow when updating this port", got[1].Quote)
	assert.Equal(t, []string{"apache-arrow"}, got[1].Ports)
}

// A refusal and an instruction in ONE block is a refusal. The direction
// of a short paragraph is the direction of its sentences, and the trade
// is one-sided on purpose: a missed instruction leaves the measurement
// to speak for itself, and a quoted refusal would hold a publication
// for a comment that asked for nothing.
func TestARefusalInTheSameBlockDeclinesIt(t *testing.T) {
	src := "# Please revbump py-gdal when this port moves.\n" +
		"# On reflection the ABI difference is too obscure to justify it.\n"
	assert.Empty(t, Instructions([]byte(src), nil))
	assert.False(t, MentionsRevbump([]byte(src)))
}

// The roster STOPS at a word that ends it rather than skipping past it.
// Skipping would let a name on the far side of the condition clause be
// read as a roster item the instruction never listed — and the names
// belong to the sentence the verb is in.
func TestTheRosterStopsRatherThanSkips(t *testing.T) {
	got := Instructions([]byte("# Please revbump libheif when ffmpeg is rebuilt.\n"), nil)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"libheif"}, got[0].Ports,
		"'when' opens the condition clause, and ffmpeg inside it is not a roster item")
}

// A clause boundary ends the roster whatever the word is, because the
// names belong to one sentence.
func TestAClauseBoundaryEndsTheRoster(t *testing.T) {
	got := Instructions([]byte("# Please revbump libheif; ffmpeg is handled upstream.\n"), nil)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"libheif"}, got[0].Ports)
}

// A comment inside a braced body is still read: privoxy's own
// instruction lives inside a subport block, and a reader that skipped
// the braced bodies would miss shapes the tree actually writes.
func TestAnInstructionInsideABracedBodyIsStillRead(t *testing.T) {
	src := "name privoxy\nsubport ${name}-pki-bundle {\n" +
		"    # Please rev-bump squirrel-ime whenever librime-devel updates\n}\n"
	got := Instructions([]byte(src), nil)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"squirrel-ime"}, got[0].Ports)
	assert.Equal(t, "    # Please rev-bump squirrel-ime whenever librime-devel updates", got[0].Quote,
		"the quote keeps its own indentation: a quote that was reflowed is not verbatim")
}

// The cheap half is asked of every port in a sweep, so it must be
// cheap AND it must be the same two patterns. A Portfile with no
// comment at all is the case it answers for 41630 entries.
func TestMentionsRevbumpIsSilentOnAnOrdinaryPortfile(t *testing.T) {
	assert.False(t, MentionsRevbump([]byte("PortSystem 1.0\nname kubectl\nversion 1.34.1\n")))
	assert.False(t, MentionsRevbump(nil))
}
