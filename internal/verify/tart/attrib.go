package tart

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// clearAttribution forgets a released worker.
func clearAttribution(vm string) {
	if path, err := attribPath(vm); err == nil {
		_ = os.Remove(path)
	}
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
