package intent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/record"
)

// The instruction-comment rule, against the comments the tree actually
// writes.
//
// Every source below is transcribed from a real Portfile, with the port
// and the line it was read at named in the row. That is not decoration:
// the plan's own sentence for this rule — "increase the revision of the
// following ports when updating" — matches ZERO Portfiles, and a rule
// written to a fixture invented alongside it would have passed its
// tests forever while finding nothing in the field.
//
// The rows that must produce NOTHING are half the table and the more
// important half. A comment that says a revbump is unnecessary, and a
// comment on a DEPENDENT saying what triggers a bump of itself, both
// match the family and mean the opposite of it — and a finding is a
// question that holds an unattended publication until a person answers
// it, so a rule that spoke for either would make the gate noise.

func TestInstructionCommentQuotesTheShapesTheTreeWrites(t *testing.T) {
	rows := []struct {
		name  string
		port  string
		src   string
		deps  []string
		ports []string // the roster, nil for the unnamed form
		quote bool     // a finding at all
	}{
		{
			name: "dav1d: a named list wrapped across two comment lines",
			port: "dav1d",
			src: "# Please increase the revision of libheif, ffmpeg and ffmpeg-devel whenever\n" +
				"# dav1d's version is updated.\n",
			ports: []string{"libheif", "ffmpeg", "ffmpeg-devel"},
			quote: true,
		},
		{
			name:  "ffmpeg: one name, one line",
			port:  "ffmpeg",
			src:   "# Please increase the revision of mpv whenever ffmpeg's version is updated.\n",
			ports: []string{"mpv"},
			quote: true,
		},
		{
			name:  "curl: the condition names the port back",
			port:  "curl",
			src:   "# Increase the revision of p5-www-curl whenever the version of curl gets updated.\n",
			ports: []string{"p5-www-curl"},
			quote: true,
		},
		{
			name:  "db48: 'any time' ends the roster",
			port:  "db48",
			src:   "# Increase the revision of p5-berkeleydb any time the db48 version changes.\n",
			ports: []string{"p5-berkeleydb"},
			quote: true,
		},
		{
			name:  "grpc: a quoted name under a NOTE: prefix",
			port:  "grpc",
			src:   "# NOTE: Also rev-bump 'apache-arrow' when updating this port\n",
			ports: []string{"apache-arrow"},
			quote: true,
		},
		{
			name:  "librime: rev-bump with the name straight after it",
			port:  "librime",
			src:   "# Please rev-bump squirrel-ime whenever librime-devel updates\n",
			ports: []string{"squirrel-ime"},
			quote: true,
		},
		{
			name: "sbcl: category prefixes, and 'possibly' inside the list",
			port: "sbcl",
			src: "# Please bump the revisions of math/maxima, math/fricas and possibly\n" +
				"# math/maxima-devel when this port changes.\n",
			ports: []string{"maxima", "fricas", "maxima-devel"},
			quote: true,
		},
		{
			name: "openssl3: a header that points at bullets, with the caveats kept in the quote",
			port: "openssl3",
			src: "# Please revbump these ports when updating the openssl3 version/revision\n" +
				"#  - freeradius (#43461)\n" +
				"#  - openssh (#54990)\n" +
				"#  - p5-net-ssleay (#67321, for minor version bumps)\n" +
				"#  - openssl (to rebuild the shim links).\n",
			ports: []string{"freeradius", "openssh", "p5-net-ssleay", "openssl"},
			quote: true,
		},
		{
			name:  "spdlog: a header and a single bullet",
			port:  "spdlog",
			src:   "# Ports that depend on this port must be revbump after update:\n# - tiledb\n",
			ports: []string{"tiledb"},
			quote: true,
		},
		{
			name: "protobuf3-cpp: the condition is on the line ABOVE the verb",
			port: "protobuf3-cpp",
			src: "# NOTE: For a minor or major version number change, also\n" +
				"# NOTE:   Revbump et, protobuf-c, mosh and py-onnx\n",
			ports: []string{"et", "protobuf-c", "mosh", "py-onnx"},
			quote: true,
		},
		{
			name: "icu: the unnamed form names nobody",
			port: "icu",
			src: "# Please increase the revision number of the dependents whenever the library\n" +
				"# version number changes.\n",
			quote: true,
		},
		{
			name: "cmark: 'all ports that link with the library' is a class, not a roster",
			port: "cmark",
			src: "# Any version update requires revbumping all ports that link with the library\n" +
				"# because the full version number is in the library's install name.\n",
			quote: true,
		},
		{
			name:  "abseil: ports that depend on this port",
			port:  "abseil",
			src:   "# Ports that depend on this port must be revbump after update.\n",
			quote: true,
		},
		{
			name: "geos: a conditional instruction about all dependents",
			port: "geos",
			src: "# NOTE: When updating this port, check whether the dylib name and/or version\n" +
				"# NOTE: changes. If so, all dependents will need to be rev-bumped.\n",
			quote: true,
		},

		// The negations. Each of these matches the family and means the
		// opposite of it.
		{
			name: "openssl3 line 147: too obscure to justify revbumping the dependents",
			port: "openssl3",
			src:  "# The ABI difference is real but is too obscure to justify\n# revbumping the dependents.\n",
		},
		{
			name: "py-sip4: no rev-bumps are needed",
			port: "py-sip4",
			src:  "#  -> SO: no rev-bumps are be needed.\n",
		},
		{
			name: "perl5: rather not revbump many p5 ports",
			port: "perl5",
			src:  "# Rather not revbump many p5 ports, so just fix it for new versions\n",
		},

		// The reverse direction. Both of these live on a DEPENDENT and
		// say what triggers a bump of it.
		{
			name: "privoxy: a bump verb with no object at all",
			port: "privoxy",
			src:  "# Please increase the revision whenever curl-ca-bundle contents change\n",
		},
		{
			name: "mpv: the comment names only the port it is written on",
			port: "mpv",
			src:  "# Please revbump mpv whenever linked ffmpeg is updated! (See ffmpeg's Portfile)\n",
		},

		// An ordinary comment, which is what 41630 Portfiles carry.
		{
			name: "an ordinary comment says nothing about revisions",
			port: "jq",
			src:  "# This port needs a C99 compiler; see the upstream README.\n",
		},

		// The index overrides the word list: a token the vocabulary would
		// have stopped on is a port when the tree already calls it one.
		{
			name:  "a dependent whose name is an English word is still a port",
			port:  "libwidget",
			src:   "# Please revbump when, whenever libwidget's version is updated.\n",
			deps:  []string{"when"},
			ports: []string{"when"},
			quote: true,
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got := instructionFindings([]byte(row.src), "devel/"+row.port, row.port, row.deps)
			if !row.quote {
				assert.Empty(t, got, "this comment must produce no finding")
				return
			}
			require.Len(t, got, 1)
			f := got[0]
			assert.Equal(t, FindingInstruction, f.Kind)
			assert.Equal(t, []string{row.port}, f.Ports)
			assert.Equal(t, "devel/"+row.port+"/Portfile", f.Source)
			assert.Equal(t, record.Proposed, f.Disposition,
				"an instruction comment is a question, and the machine gate holds an unattended publication until it is answered")
			assert.Equal(t, strings.TrimRight(row.src, "\n"), f.Quote,
				"the quote is the whole comment block, byte for byte")
			assert.Empty(t, f.Criterion,
				"the criterion of this finding IS its quote; writing it into two keys would be two places for it to drift")

			var named []string
			for _, c := range f.Candidates {
				named = append(named, c.Port)
				assert.False(t, c.Proposed, "a comment names candidates and proposes nobody")
				assert.Contains(t, c.Reason, "devel/"+row.port+"/Portfile")
			}
			assert.Equal(t, row.ports, named)
		})
	}
}

// The unnamed form is the one that must add nobody. Reading "all
// dependents will need to be rev-bumped" as a roster would auto-include
// every dependent there is off one sentence, which is the one thing
// this tool must never do.
func TestTheUnnamedInstructionNamesNobody(t *testing.T) {
	got := instructionFindings(
		[]byte("# Please increase the revision number of the dependents whenever the library\n# version number changes.\n"),
		"devel/icu", "icu", []string{"harfbuzz", "boost", "qt5-qtbase"})
	require.Len(t, got, 1)
	assert.Empty(t, got[0].Candidates,
		"a criterion is not a roster: the dependents are named by the index and weighed by the measurement, never by this")
	assert.NotEmpty(t, got[0].Quote)
}

// Two instructions in one Portfile are two findings, because they are
// two sentences a person weighs separately — and a block ends at the
// first line that is not a comment, so a negation further down the file
// cannot silence an instruction above it.
func TestTwoCommentBlocksAreTwoFindings(t *testing.T) {
	src := "# Please revbump mpv whenever ffmpeg's version is updated.\n" +
		"name ffmpeg\n" +
		"# Also rev-bump apache-arrow when updating this port\n" +
		"version 7.1\n"
	got := instructionFindings([]byte(src), "multimedia/ffmpeg", "ffmpeg", nil)
	require.Len(t, got, 2)
	assert.Equal(t, "# Please revbump mpv whenever ffmpeg's version is updated.", got[0].Quote)
	assert.Equal(t, "# Also rev-bump apache-arrow when updating this port", got[1].Quote)
}

// A comment inside a braced body is still read. The rule that cares
// where a comment sits is the rider's first proof, which is about
// EDITING bytes; this only reads them, and privoxy's own instruction
// lives inside a subport block.
func TestAnInstructionInsideABracedBodyIsStillRead(t *testing.T) {
	src := "name privoxy\nsubport ${name}-pki-bundle {\n" +
		"    # Please rev-bump squirrel-ime whenever librime-devel updates\n}\n"
	got := instructionFindings([]byte(src), "www/privoxy", "privoxy", nil)
	require.Len(t, got, 1)
	require.Len(t, got[0].Candidates, 1)
	assert.Equal(t, "squirrel-ime", got[0].Candidates[0].Port)
	assert.Equal(t, "    # Please rev-bump squirrel-ime whenever librime-devel updates", got[0].Quote,
		"the quote keeps its own indentation: a quote that was reflowed is not verbatim")
}

// The source is the portdir a reader would cite and not the path on
// this machine, because a note outlives the checkout that wrote it.
func TestTheSourceIsTheCitablePortdir(t *testing.T) {
	src := "# Please rev-bump squirrel-ime whenever librime-devel updates\n"
	for portdir, want := range map[string]string{
		"/Users/x/Source/macports-ports/chinese/librime": "chinese/librime/Portfile",
		"chinese/librime": "chinese/librime/Portfile",
		"librime":         "librime/Portfile",
	} {
		got := instructionFindings([]byte(src), portdir, "librime", nil)
		require.Len(t, got, 1, portdir)
		assert.Equal(t, want, got[0].Source)
	}
}

// Examine is the seam a planner meets, and the finding reaches it with
// the rider beside it: one look at the Portfile answers both.
func TestExamineCarriesTheInstructionFinding(t *testing.T) {
	src, cst := parsed(t, "# Please increase the revision of mpv whenever ffmpeg's version is updated.\nname ffmpeg\n")
	ex := Examine(Portfile{Src: src, CST: cst, Portdir: "multimedia/ffmpeg",
		Vals: info.Values{Name: "ffmpeg"}})
	require.Len(t, ex.Findings, 1)
	assert.Equal(t, FindingInstruction, ex.Findings[0].Kind)
	require.Len(t, ex.Findings[0].Candidates, 1)
	assert.Equal(t, "mpv", ex.Findings[0].Candidates[0].Port)
	assert.Len(t, ex.Riders, 1, "the modeline still rides: one look answers both rules")
}
