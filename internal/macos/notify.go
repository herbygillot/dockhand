package macos

import (
	"context"
	"errors"
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

// Opener opens addresses in the person's browser with open(1), at Path,
// which the command line finds on the PATH dockhand was run with.
type Opener struct{ Path string }

// Open opens an address in the person's browser.
func (o Opener) Open(ctx context.Context, url string) error {
	if o.Path == "" {
		return errors.New("macos: open isn't on the PATH dockhand was run with")
	}
	_, err := subprocess.Run(ctx, subprocess.Spec{Tool: "open", Path: o.Path, Args: []string{url}, Limit: 1 << 16})
	return err
}
