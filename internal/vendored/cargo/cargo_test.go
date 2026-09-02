package cargo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/vendored"
)

// tools is the finder the generator and the archiver resolve through:
// the real one, because these tests run the real cargo2port and read
// real tarballs, gated by testenv where the tool may be absent.
var tools = tool.NewFinder(nil)

func TestCratesParsesTriples(t *testing.T) {
	crates, err := Crates("aho-corasick 1.1.3 8e60d34 libgit2-sys 0.17.0+1.8.1 10472326")
	require.NoError(t, err)
	require.Len(t, crates, 2)
	assert.Equal(t, Crate{"aho-corasick", "1.1.3", "8e60d34"}, crates[0])
	// Build metadata in a version passes through, as the PortGroup's own
	// ${cname}-${cversion}.crate construction does.
	assert.Equal(t, "libgit2-sys-0.17.0+1.8.1.crate", crates[1].Distfile())
}

func TestCratesRejectsPartialTriple(t *testing.T) {
	_, err := Crates("aho-corasick 1.1.3")
	require.ErrorIs(t, err, vendored.ErrMalformed)
}

func TestCratesEmpty(t *testing.T) {
	crates, err := Crates("")
	require.NoError(t, err)
	assert.Empty(t, crates)
}

func TestSuppliedFeedsSubtraction(t *testing.T) {
	crates, err := Crates("aho-corasick 1.1.3 8e60d34 bitflags 2.6.0 b048fb6")
	require.NoError(t, err)
	own, err := vendored.Own([]string{
		"tokei-13.0.0.tar.gz",
		"aho-corasick-1.1.3.crate:crate-aho-corasick-8e60d34",
		"bitflags-2.6.0.crate:crate-bitflags-b048fb6",
	}, Supplied(crates))
	require.NoError(t, err)
	assert.Equal(t, []string{"tokei-13.0.0.tar.gz"}, own)
}

const oneCrateLock = `version = 3

[[package]]
name = "aho-corasick"
version = "1.1.3"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916"
`

func TestGenerateWritesABlock(t *testing.T) {
	testenv.Tool(t, ToolName)
	block, err := Generate(context.Background(), tools, tempdir.Root{}, []byte(oneCrateLock), LayoutJustify)
	require.NoError(t, err)
	assert.Contains(t, string(block), "cargo.crates")
	assert.Contains(t, string(block), "aho-corasick")
	assert.Contains(t, string(block), "8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916")

	// The block must parse back as the triples the PortGroup reads.
	crates, err := Crates(afterCommandName(string(block)))
	require.NoError(t, err)
	require.Len(t, crates, 1)
	assert.Equal(t, "aho-corasick-1.1.3.crate", crates[0].Distfile())
}

// A lockfile with no checksummed packages is the silent-success case:
// the tool says so on stdout and exits zero.
func TestGenerateRejectsLockWithNoCrates(t *testing.T) {
	testenv.Tool(t, ToolName)
	_, err := Generate(context.Background(), tools, tempdir.Root{}, []byte("version = 3\n"), LayoutJustify)
	require.ErrorIs(t, err, vendored.ErrEmptyBlock)
}

func TestGenerateReportsToolFailure(t *testing.T) {
	testenv.Tool(t, ToolName)
	_, err := Generate(context.Background(), tools, tempdir.Root{}, []byte("this is not toml\n"), LayoutJustify)
	require.Error(t, err)
	assert.NotErrorIs(t, err, vendored.ErrEmptyBlock, "a parse failure is not an empty block")
}

// scripted is a finder whose cargo2port is a shell script with the
// given body, every other lookup answered for real.
func scripted(t *testing.T, body string) *tool.Finder {
	t.Helper()
	path := filepath.Join(t.TempDir(), ToolName)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Cargo2Port) {
			return path, nil
		}
		return exec.LookPath(name)
	})
}

func TestGenerateNamesTheGeneratorWhenItIsMissing(t *testing.T) {
	absent := tool.NewFinder(func(string) (string, error) { return "", errors.New("absent") })
	_, err := Generate(context.Background(), absent, tempdir.Root{}, []byte(oneCrateLock), LayoutJustify)
	require.ErrorIs(t, err, vendored.ErrNoGenerator)
	assert.Equal(t, "vendored: block generator not found: cargo2port", err.Error())
}

func TestGenerateWordsAToolFailureWithItsStderr(t *testing.T) {
	scripted := scripted(t, "echo 'error: could not parse Cargo.lock' >&2\nexit 1\n")
	_, err := Generate(context.Background(), scripted, tempdir.Root{}, []byte(oneCrateLock), LayoutJustify)
	require.Error(t, err)
	assert.Equal(t, "vendored: cargo2port: error: could not parse Cargo.lock", err.Error())
}

// afterCommandName strips the leading "cargo.crates" and the line
// continuations, leaving the word list the option evaluates to.
func afterCommandName(block string) string {
	body := block[len(vendored.CargoCrates.String()):]
	out := make([]rune, 0, len(body))
	for _, r := range body {
		if r == '\\' || r == '\n' {
			out = append(out, ' ')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// The layout is requested, not accepted: the tool's default is a third
// thing that matches neither named mode, and regenerating a justified
// block without asking would rewrite every line in it.
func TestGenerateRequestsTheTreesAlignment(t *testing.T) {
	testenv.Tool(t, ToolName)
	// Names and versions of differing widths, so the layouts diverge.
	const lock = `version = 3

[[package]]
name = "aho-corasick"
version = "1.1.3"
checksum = "8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916"

[[package]]
name = "libgit2-sys"
version = "0.17.0+1.8.1"
checksum = "10472326a8a6477c3c20a64547b0059e4b0d086869eee31e6d7da728a8eb7224"
`
	block, err := Generate(context.Background(), tools, tempdir.Root{}, []byte(lock), LayoutJustify)
	require.NoError(t, err)

	// Justified means the version column is right-aligned: every version
	// ends at the same offset, while the names start at the same one.
	var ends, starts []int
	for _, line := range strings.Split(string(block), "\n")[1:] {
		fields := strings.Fields(strings.TrimSuffix(strings.TrimSpace(line), `\`))
		if len(fields) != 3 {
			continue
		}
		starts = append(starts, strings.Index(line, fields[0]))
		ends = append(ends, strings.Index(line, fields[1])+len(fields[1]))
	}
	require.Len(t, ends, 2)
	assert.Equal(t, ends[0], ends[1], "versions must end at a common column")
	assert.Equal(t, starts[0], starts[1], "names must start at a common column")
}
