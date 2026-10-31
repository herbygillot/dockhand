package testsupport

import (
	"os"
	"path/filepath"
	"testing"
)

// WriteFiles writes each of files, by its slash-separated path, under
// root, making the directories it needs, so a test lays out a ports tree,
// a source directory, or a configuration in one call.
func WriteFiles(t testing.TB, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
