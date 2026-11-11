package project

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/archive"
)

// Inventory is what an archive holds below its one top directory
// (archive.Tops), every file whole, for telling one archive's contents
// from another's: its regular files, each with its contents' digest
// (archive.Member.Digest), its symbolic links, each with its target, each
// directory with how many regular files are under it, and .gitmodules'
// text where it has one. Top is empty, and the rest too, where the archive
// has no one top directory.
//
// It keeps what Read doesn't, directories and links and every file's
// digest, and isn't cached; the two share the walk and the top rule.
type Inventory struct {
	Top        string
	Files      map[string]string
	Links      map[string]string
	Dirs       map[string]int
	Gitmodules []byte
}

// gitmodulesLimit bounds the .gitmodules text an inventory keeps.
const gitmodulesLimit = 1 << 20

// ReadInventory reads an archive's inventory.
func ReadInventory(ctx context.Context, filename string) (Inventory, error) {
	files, links, dirs := map[string]string{}, map[string]string{}, map[string]bool{}
	gitmodules := map[string][]byte{}
	tops, err := walk(ctx, filename, func(member archive.Member, name string) error {
		if !member.Regular {
			if member.Link != "" {
				links[name] = member.Link
			}
			dirs[name] = true
			return nil
		}
		first, rest, _ := strings.Cut(name, "/")
		var kept *cappedBuffer
		if rest == ".gitmodules" {
			kept = &cappedBuffer{limit: gitmodulesLimit}
			member.Body = io.TeeReader(member.Body, kept)
		}
		digest, err := member.Digest()
		if err != nil {
			return err
		}
		files[name] = digest
		if kept != nil {
			gitmodules[first] = kept.Bytes()
		}
		return nil
	})
	if err != nil {
		return Inventory{}, err
	}
	inventory := Inventory{Files: map[string]string{}, Links: map[string]string{}, Dirs: map[string]int{}}
	top, one := tops.One()
	if !one {
		return inventory, nil
	}
	inventory.Top, inventory.Gitmodules = top, gitmodules[top]
	below := func(name string) (string, bool) { return strings.CutPrefix(name, top+"/") }
	for name, digest := range files {
		if rest, ok := below(name); ok {
			inventory.Files[rest] = digest
			for dir := path.Dir(rest); dir != "."; dir = path.Dir(dir) {
				inventory.Dirs[dir]++
			}
		}
	}
	for name, target := range links {
		if rest, ok := below(name); ok {
			inventory.Links[rest] = target
		}
	}
	for name := range dirs {
		if rest, ok := below(name); ok {
			if _, counted := inventory.Dirs[rest]; !counted {
				inventory.Dirs[rest] = 0
			}
		}
	}
	return inventory, nil
}

// Has reports a file or directory below the top. A link counts as a
// directory.
func (i Inventory) Has(name string) bool {
	_, file := i.Files[name]
	_, dir := i.Dirs[name]
	return file || dir
}

// Submodules are the paths .gitmodules names, in its order.
func (i Inventory) Submodules() []string {
	var paths []string
	scanner := bufio.NewScanner(bytes.NewReader(i.Gitmodules))
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if ok && strings.TrimSpace(key) == "path" {
			if name := strings.Trim(strings.TrimSpace(value), "/"); name != "" && !slices.Contains(paths, name) {
				paths = append(paths, name)
			}
		}
	}
	return paths
}

// cappedBuffer keeps the first limit bytes written to it and takes the
// rest unkept.
type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
