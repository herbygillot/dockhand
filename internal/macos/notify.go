package macos

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/subprocess"
)

// notificationTimeout bounds posting one notification, so an osascript
// that never returns is stopped rather than left running.
const notificationTimeout = 10 * time.Second

// Notify posts a notification, titled "dockhand · title". It's posted
// through AppleScript, so macOS credits it to Script Editor.
func Notify(ctx context.Context, title, text string) error {
	quote := func(value string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"` }
	ctx, cancel := context.WithTimeout(ctx, notificationTimeout)
	defer cancel()
	_, err := subprocess.Run(ctx, subprocess.Spec{Tool: "osascript", Path: "osascript", Args: []string{"-e", "display notification " + quote(text) + " with title " + quote("dockhand · "+title)}, Limit: 1 << 16})
	return err
}

// Open opens an address in the person's browser, with open(1).
func Open(ctx context.Context, url string) error {
	path, err := exec.LookPath("open")
	if err != nil {
		return err
	}
	_, err = subprocess.Run(ctx, subprocess.Spec{Tool: "open", Path: path, Args: []string{url}, Limit: 1 << 16})
	return err
}
