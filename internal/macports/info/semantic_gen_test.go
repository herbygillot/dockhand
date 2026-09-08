package info

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheComparisonTableIsWhatTheGeneratorWrites is the second half of
// the prediction contract's enforcement, and it guards the direction the
// unkeyed pins in semantic_compare_gen.go cannot.
//
// The pins catch a field ADDED to Semantic without regeneration: an
// unkeyed composite literal must name every field, so the package stops
// compiling. What they cannot catch is the generated file being edited
// by hand — a row deleted from semanticTable, a Field's MacPorts name
// changed to something the struct tag does not say — because such a file
// still compiles and its pins are still complete. This test runs the
// generator the //go:generate directive names and requires the committed
// file to be byte-identical to what it produces, so the file is a
// FUNCTION of the struct in both directions.
//
// It runs the real command rather than calling a library, which is the
// point: what is proved is that `go run ./internal/gen` from this
// directory produces this file. A test that called the generator's
// internals could pass while the directive was broken.
func TestTheComparisonTableIsWhatTheGeneratorWrites(t *testing.T) {
	goBin, err := exec.LookPath("go")
	require.NoError(t, err, "the toolchain running this test is the toolchain the directive runs")

	out := filepath.Join(t.TempDir(), "regenerated.go")
	cmd := exec.Command(goBin, "run", "./internal/gen", "-o", out)
	// The directive's own working directory: go:generate runs a command
	// in the directory of the file that carries it, and that is what
	// makes "./internal/gen" and the default output path resolve.
	cmd.Dir = "."
	combined, err := cmd.CombinedOutput()
	require.NoError(t, err, "the generator did not run: %s", combined)

	want, err := os.ReadFile(out)
	require.NoError(t, err)
	got, err := os.ReadFile("semantic_compare_gen.go")
	require.NoError(t, err)

	assert.Equal(t, string(want), string(got),
		"semantic_compare_gen.go is not what the generator writes; "+
			"run `go generate ./internal/macports/info` and commit the result")
}

// TestEveryFieldOfSemanticIsCompared states the contract the generation
// exists to hold, in the vocabulary a reader of this package has: the
// number of comparable fields is the number of leaves in Semantic, and
// the table, the Field set and Fields() all agree on it.
//
// It is deliberately arithmetic and not a list. A list would be a third
// copy of the same table and would need editing every time the first two
// were regenerated, which is the maintenance burden this whole step is
// about removing. Counting proves the invariant without restating it.
func TestEveryFieldOfSemanticIsCompared(t *testing.T) {
	assert.Len(t, Fields(), len(semanticTable),
		"Fields() and the comparison table are generated from one struct and must agree")

	seen := map[Field]bool{}
	for _, row := range semanticTable {
		assert.False(t, seen[row.field], "field %s has two rows in the comparison table", row.field)
		seen[row.field] = true
		assert.NotEqual(t, "unknown field", row.field.String(),
			"field %d has a table row but no MacPorts name", int(row.field))
	}
	for _, f := range Fields() {
		assert.True(t, seen[f], "field %s is declared but never compared", f)
	}
}

// TestAFieldAddedToSemanticWithoutAComparisonIsCaught proves the pin is
// real by doing the thing it exists to catch: it hands the generator a
// package whose Semantic has an extra field, and requires the file the
// generator writes to differ from the committed one.
//
// The compile-time half cannot be tested from inside the package it
// would stop compiling, so what is tested is the half that CAN be
// observed at run time — that the generator notices, and that the
// regeneration diff above would therefore fail. The observation is
// made in a scratch copy of the source, so the real declarations are
// never touched.
func TestAFieldAddedToSemanticWithoutAComparisonIsCaught(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile("semantic.go")
	require.NoError(t, err)
	// A new leaf, tagged the way a real one would be, appended to the
	// struct exactly as a person adding a field would append it.
	patched := replaceLast(string(src), "\tDepends Depends `field:\"depends\"`\n",
		"\tDepends Depends `field:\"depends\"`\n\tSubversion string `field:\"subversion\"`\n")
	require.NotEqual(t, string(src), patched, "the fixture must actually change the struct")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "semantic.go"), []byte(patched), 0o600))

	goBin, err := exec.LookPath("go")
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "regenerated.go")
	cmd := exec.Command(goBin, "run", "./internal/gen", "-dir", dir, "-o", out)
	cmd.Dir = "."
	combined, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", combined)

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(got), "FieldSubversion",
		"a field added to Semantic must appear in the regenerated table")

	committed, err := os.ReadFile("semantic_compare_gen.go")
	require.NoError(t, err)
	assert.NotEqual(t, string(committed), string(got),
		"the regeneration diff must fail for a field added without regenerating")
}

// TestTheGeneratorRefusesAnUntaggedField pins the other refusal: a
// MacPorts option name is never invented from a Go identifier, because
// that name is what a plan's durable wire form records and what
// change.fieldNamed inverts. A field with no tag stops the generator
// rather than producing a name nobody declared (rule 6).
func TestTheGeneratorRefusesAnUntaggedField(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile("semantic.go")
	require.NoError(t, err)
	patched := replaceLast(string(src), "\tDepends Depends `field:\"depends\"`\n",
		"\tDepends Depends `field:\"depends\"`\n\tSubversion string\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "semantic.go"), []byte(patched), 0o600))

	goBin, err := exec.LookPath("go")
	require.NoError(t, err)
	cmd := exec.Command(goBin, "run", "./internal/gen", "-dir", dir,
		"-o", filepath.Join(t.TempDir(), "out.go"))
	cmd.Dir = "."
	combined, err := cmd.CombinedOutput()
	require.Error(t, err, "an untagged field must stop the generator")
	assert.Contains(t, string(combined), "Semantic.Subversion")
}

// replaceLast is the fixture's own splice: the struct's last field is
// where a person appends, so that is where these tests append.
func replaceLast(s, old, new string) string {
	i := len(s) - len(old)
	for ; i >= 0; i-- {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}
