package tart

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// attribution is the informational sidecar mapping a worker to the
// checkout that started it. Informational on purpose: admission never
// reads it, so staleness can only mislabel a status line, never
// mis-admit a VM. Written at submit, removed at release; an entry
// orphaned by a crash is cross-checked against `tart list` by every
// reader anyway.
type attribution struct {
	Repo    string    `json:"repo"`
	Started time.Time `json:"started"`
}

func attribPath(vm string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workers", vm+".json"), nil
}

// writeAttribution records the owning checkout, best-effort: a worker
// without a record just reads as unattributed.
func writeAttribution(vm, repo string) {
	if repo == "" {
		return
	}
	path, err := attribPath(vm)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	b, err := json.Marshal(attribution{Repo: repo, Started: time.Now()})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0o644)
}

// clearAttribution forgets a released worker, and its last words with
// it: a worker that has gone back has nothing left to explain.
func clearAttribution(vm string) {
	if path, err := attribPath(vm); err == nil {
		_ = os.Remove(path)
	}
	if path, err := exitPath(vm); err == nil {
		_ = os.Remove(path)
	}
}

// exitPath is where a worker's LAST WORDS are kept: what `tart run`
// printed and the status it ended with.
//
// It sits beside the attribution sidecar because it answers the same
// kind of question about the same key, and because the HOST is the only
// party still able to answer it. A guest that has stopped cannot say
// why it stopped; the process that WAS the virtual machine can, and
// used to say it into a channel nobody read after startup.
func exitPath(vm string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workers", vm+".exit"), nil
}

// exitNoteMax bounds the note, because where it is going is a record
// Detail somebody reads in a status line and not a log.
const exitNoteMax = 200

// noteExit records how the virtual machine's own process ended.
//
// Both halves are kept because tart uses both: a refusal arrives as a
// non-zero status, and a guest that stopped on its own arrives as a
// sentence on stdout with a zero one. Nothing is written when there is
// nothing to say, so ExitOf's empty answer means "it said nothing" and
// never "nobody looked" — rule 7, for the one question a crashed run
// leaves behind.
func noteExit(vm, out string, err error) {
	say := strings.Join(strings.Fields(out), " ")
	switch {
	case err != nil && say != "":
		say += " (" + err.Error() + ")"
	case err != nil:
		say = err.Error()
	}
	if say == "" {
		return
	}
	if len(say) > exitNoteMax {
		say = say[:exitNoteMax] + "\u2026"
	}
	path, perr := exitPath(vm)
	if perr != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(say), 0o644)
}

// ExitOf is what a worker's own process said when it stopped, "" when
// it said nothing or nothing was kept.
func ExitOf(vm string) string {
	path, err := exitPath(vm)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// OwnerOf names the checkout that started a worker, "" when nothing
// says.
func OwnerOf(vm string) string {
	path, err := attribPath(vm)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var a attribution
	if json.Unmarshal(b, &a) != nil {
		return ""
	}
	return a.Repo
}
