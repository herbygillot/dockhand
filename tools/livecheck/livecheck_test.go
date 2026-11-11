package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// fixture is a port that found a bug, recorded once: its options as a
// survey has them, its repository's tags and its pages, and the version
// Base's livecheck reads from them.
type fixture struct {
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Options   map[string]string `json:"options"`
	Recording recording         `json:"recording"`
	Answer    string            `json:"answer"`
}

func loadFixture(t *testing.T, name string) (macports.PortInfo, recording, string) {
	t.Helper()
	dir := filepath.Join("testdata", name)
	data, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	require.NoError(t, err)
	var f fixture
	require.NoError(t, json.Unmarshal(data, &f))
	// A page is recorded in a file of its own beside the fixture.
	for url, file := range f.Recording.Pages {
		page, err := os.ReadFile(filepath.Join(dir, file))
		require.NoError(t, err)
		f.Recording.Pages[url] = string(page)
	}
	return macports.PortInfo{Name: f.Name, Version: f.Version, Options: f.Options}, f.Recording, f.Answer
}

// Each fixture's port answers as Base does from its recording: pinact's
// livecheck read nothing before 31fca1db, its own tag missed, and reads
// 5.0.2 since.
func TestFixturesAnswerAsBaseDoes(t *testing.T) {
	t.Parallel()
	versions := &eval.Evaluator{Executable: testsupport.MacPortsTclsh(t), Adapter: testsupport.BaseAdapter()}
	port, recorded, answer := loadFixture(t, "pinact")
	got, detail := replay(t.Context(), port, recorded, versions)
	require.Equal(t, answered, got, detail)
	require.Equal(t, answer, detail)
}

// With only its own tag recorded, as before any capture, a port whose
// livecheck reads a page asks for one, which is a replay miss and no
// failure; a port whose filter admits its own tag answers. Neither is
// own tag missed, which pinact was before 31fca1db (#200).
func TestTheOwnTagCheckNeedsNoNetwork(t *testing.T) {
	t.Parallel()
	versions := &eval.Evaluator{Executable: testsupport.MacPortsTclsh(t), Adapter: testsupport.BaseAdapter()}
	port, _, _ := loadFixture(t, "pinact")
	recorded, ok := ownTag(port)
	require.True(t, ok)
	require.Equal(t, []string{"v5.0.0"}, recorded.Tags)
	got, detail := replay(t.Context(), port, recorded, versions)
	require.Equal(t, replayMiss, got, detail)

	plain := port
	plain.Options = map[string]string{}
	for key, value := range port.Options {
		plain.Options[key] = value
	}
	plain.Options["livecheck.regex"] = `{archive/refs/tags/v([^/]+)\.tar\.gz}`
	recorded, _ = ownTag(plain)
	got, detail = replay(t.Context(), plain, recorded, versions)
	require.Equal(t, answered, got, detail)
	require.Equal(t, "5.0.0", detail)
}
