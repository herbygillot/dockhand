package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
)

// Stealth is a stealth update (Design v3 §6.5): a distfile that changed
// upstream without a new name.
type Stealth struct {
	Distfiles []StealthDistfile
	// DistSubdir is where mirrors now keep the new archive, as evaluated,
	// such as croc/10.2.4_1; empty when it was not set, and Problem says
	// why.
	DistSubdir string
	Problem    string
}

// StealthDistfile is one archive's checksums before and after.
type StealthDistfile struct {
	Name     string
	Was, Now portfile.Checksum
}

// stealth finds a stealth update in a checksum refresh: an archive whose
// contents changed under the same name, in a Portfile the branch has not
// changed since its base. A Portfile edited by hand first, as for a new
// version, is not one. For a stealth update it sets dist_subdir in the
// prepared Portfile, the MacPorts guide's recipe, so mirrors keep both
// archives, and rewrites the prepared tree to match.
func (e *Engine) stealth(ctx context.Context, worktree *git.Repository, branch model.Branch, captured string, port string, result *preparation.Result) (*Stealth, error) {
	index := -1
	for i, file := range result.Files {
		if strings.HasSuffix(file.Path, "/Portfile") && (result.Target.Portfile == "" || file.Path == result.Target.Portfile) && !file.Delete {
			index = i
			break
		}
	}
	if index < 0 || len(result.Fidelity) == 0 || len(result.Downloads) == 0 {
		return nil, nil
	}
	edit := &result.Files[index]
	trees, err := worktree.CommitTrees(ctx, []string{string(branch.Base)})
	if err != nil {
		return nil, err
	}
	atBase, _, err := worktree.File(ctx, trees[string(branch.Base)], edit.Path)
	if err != nil {
		return nil, err
	}
	if !atBase.Exists || atBase.Blob != edit.Before.Blob {
		return nil, nil
	}
	was := declaredChecksums(result.Fidelity[0].Before.Ports[port].Options["checksums"])
	var found Stealth
	for _, download := range result.Downloads {
		before, ok := was[download.Name]
		if !ok && len(was) == 1 && len(result.Downloads) == 1 {
			before, ok = was[""]
		}
		if ok && changedContents(before, download.Checksum) {
			found.Distfiles = append(found.Distfiles, StealthDistfile{Name: download.Name, Was: before, Now: download.Checksum})
		}
	}
	if len(found.Distfiles) == 0 {
		return nil, nil
	}
	after, n, err := portfile.StealthDistSubdir(edit.After)
	if err != nil {
		found.Problem = strings.TrimPrefix(err.Error(), portfile.ErrUnsupported.Error()+": ")
		return &found, nil
	}
	edit.After = after
	tree, err := worktree.EditTree(ctx, captured, result.Files)
	if err != nil {
		return nil, err
	}
	result.PreparedTree = model.ObjectID(tree)
	version := result.Fidelity[len(result.Fidelity)-1].After.Ports[port].Version
	found.DistSubdir = fmt.Sprintf("%s/%s_%d", port, version, n)
	return &found, nil
}

// declaredChecksums reads an evaluated checksums option, by distfile:
// "rmd160 … sha256 … size …" for one unnamed distfile, or each distfile's
// name followed by its checksums.
func declaredChecksums(value string) map[string]portfile.Checksum {
	declared := map[string]portfile.Checksum{}
	name := ""
	words := strings.Fields(value)
	for i := 0; i < len(words); i++ {
		word := words[i]
		if !portfile.IsChecksumKind(word) || i+1 == len(words) {
			name = word
			continue
		}
		i++
		sum := declared[name]
		sum.Name = name
		switch word {
		case "rmd160":
			sum.RMD160 = words[i]
		case "sha256":
			sum.SHA256 = words[i]
		case "size":
			sum.Size, _ = strconv.ParseInt(words[i], 10, 64)
		case "md5":
			sum.MD5 = words[i]
		case "sha1":
			sum.SHA1 = words[i]
		}
		declared[name] = sum
	}
	return declared
}

// changedContents compares what both declare.
func changedContents(was, now portfile.Checksum) bool {
	return was.SHA256 != "" && was.SHA256 != now.SHA256 ||
		was.RMD160 != "" && was.RMD160 != now.RMD160 ||
		was.Size != 0 && was.Size != now.Size
}
