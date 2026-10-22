package settings

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/scratch"
)

// EditorCommand is the person's editor, $VISUAL or else $EDITOR, as a
// shell command; empty where neither is set.
func EditorCommand(s *Settings) string {
	return cmp.Or(s.Getenv("VISUAL"), s.Getenv("EDITOR"))
}

// RunEditor opens path in editor on the terminal dockhand runs on, and
// waits for it.
func RunEditor(ctx context.Context, editor, path string) error {
	command := exec.CommandContext(ctx, "sh", "-c", editor+` "$1"`, "sh", path)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("the editor failed: %w", err)
	}
	return nil
}

// EditText opens text in $VISUAL or $EDITOR and returns what was saved.
func EditText(ctx context.Context, editor, text string) (string, error) {
	if editor == "" {
		return "", errors.New("Set $VISUAL or $EDITOR to edit the description here, or edit it on GitHub after submitting. Dockhand preserves your edits.")
	}
	directory, err := scratch.Dir("description-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "DESCRIPTION.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	if err := RunEditor(ctx, editor, path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	return string(data), err
}
