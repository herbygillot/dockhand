package cli

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/scratch"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// editMessage opens the person's editor on a commit message and returns
// what they saved, with comment lines removed, the way git commit does. The
// editor is VISUAL, then EDITOR, then vi.
func editMessage(ctx context.Context, initial string) (string, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	directory, err := scratch.Dir("message-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "COMMIT_EDITMSG")
	text := strings.TrimRight(initial, "\n") + "\n\n# The first line is the commit subject, and the pull request title.\n# Lines starting with # are removed. An empty message stops the amendment.\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "sh", "-c", editor+` "$1"`, "dockhand-editor", path)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("editor %s: %w", editor, err)
	}
	edited, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var kept []string
	for _, line := range strings.Split(string(edited), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	}
	message := strings.TrimSpace(strings.Join(kept, "\n"))
	if message == "" {
		return "", fmt.Errorf("the edited message is empty; the amendment stops here")
	}
	return message + "\n", nil
}
