package change

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memTree is a working tree with no disk under it, which is the point of
// Tree being an interface: the failure policy is decidable here without
// a repository.
type memTree struct {
	files  map[string][]byte
	failOn string // the path whose Write or Remove blows up
	rigid  bool   // and a restore that fails too
}

func (m *memTree) Read(p string) ([]byte, bool, error) {
	c, ok := m.files[p]
	return c, ok, nil
}

func (m *memTree) Write(p string, c []byte, _ fs.FileMode) error {
	if p == m.failOn || (m.rigid && m.failOn != "") {
		return errors.New("disk says no")
	}
	m.files[p] = c
	return nil
}

func (m *memTree) Remove(p string) error {
	if p == m.failOn {
		return errors.New("disk says no")
	}
	delete(m.files, p)
	return nil
}

func tree(kv map[string][]byte) *memTree { return &memTree{files: kv} }

// THE WHOLE SET LANDS, which is the ordinary case and includes the file
// kinds a bare loop handled: a rewrite, a creation and a deletion.
func TestApplyToWritesEveryFileTheChangeCarries(t *testing.T) {
	m := tree(map[string][]byte{"p/Portfile": []byte("version 1"), "p/files/old.diff": []byte("x")})
	p := Prepared{Files: []File{
		{Path: "p/Portfile", Content: []byte("version 2")},
		{Path: "p/files/new.diff", Content: []byte("y")},
		{Path: "p/files/old.diff", Delete: true},
	}}
	require.NoError(t, p.ApplyTo(m))
	assert.Equal(t, "version 2", string(m.files["p/Portfile"]))
	assert.Equal(t, "y", string(m.files["p/files/new.diff"]))
	assert.NotContains(t, m.files, "p/files/old.diff")
}

// ALL OR NOTHING. The loop this replaced returned on the first error and
// left whatever it had already written, which for a working tree is
// recoverable — and "recoverable" is a worse contract than "unchanged",
// because a half-applied change is one a person can commit by accident.
func TestApplyToLeavesTheTreeUntouchedWhenAnyFileFails(t *testing.T) {
	m := tree(map[string][]byte{"p/Portfile": []byte("version 1"), "p/files/keep.diff": []byte("x")})
	m.failOn = "p/files/new.diff"
	before := map[string]string{}
	for k, v := range m.files {
		before[k] = string(v)
	}

	p := Prepared{Files: []File{
		{Path: "p/Portfile", Content: []byte("version 2")},
		{Path: "p/files/keep.diff", Delete: true},
		{Path: "p/files/new.diff", Content: []byte("y")},
	}}
	require.Error(t, p.ApplyTo(m))

	after := map[string]string{}
	for k, v := range m.files {
		after[k] = string(v)
	}
	assert.Equal(t, before, after, "the earlier write AND the earlier delete are both put back")
}

// A RESTORE THAT ALSO FAILS IS ITS OWN FACT. A refusal leaves a tree as
// it was; this leaves one somebody has to look at, and saying so is the
// only honest answer left.
func TestApplyToSaysSoWhenItCannotPutThingsBack(t *testing.T) {
	m := tree(map[string][]byte{"p/Portfile": []byte("version 1")})
	m.failOn, m.rigid = "p/files/new.diff", true
	p := Prepared{Files: []File{
		{Path: "p/Portfile", Content: []byte("version 2")},
		{Path: "p/files/new.diff", Content: []byte("y")},
	}}
	err := p.ApplyTo(m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPartiallyApplied)
}

// A DELETE OF SOMETHING ALREADY GONE IS THE STATE ASKED FOR, not an
// error: the tree is where the change wants it.
func TestApplyToAcceptsADeleteOfAnAbsentFile(t *testing.T) {
	m := tree(map[string][]byte{})
	require.NoError(t, Prepared{Files: []File{{Path: "p/files/gone.diff", Delete: true}}}.ApplyTo(m))
}
