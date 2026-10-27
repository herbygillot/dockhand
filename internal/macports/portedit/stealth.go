package portedit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/archive"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/scratch"
)

// StealthRequest asks a checksum refresh to treat an archive whose contents
// changed upstream under the same name as a stealth update (Design v3 §6.5):
// the revision bumped, since the source changed, unless KeepRevision, and
// dist_subdir set so mirrors keep both archives. Changed are the files the
// branch has changed since its base, which the caller knows, and Base the
// port as the base evaluates it, where Changed names its Portfile: a
// version edited by hand since, as for a new version, makes the refresh no
// stealth update, while a comment or a homepage edited leaves it one (the
// update-workflow review's efficiency note). Base is nil where the base has
// no such port, or it couldn't be evaluated, and a Portfile changed then is
// taken as a new version.
type StealthRequest struct {
	Changed      []string
	KeepRevision bool
	Base         *macports.PortInfo
}

// Stealth is the stealth update a checksum refresh found and made, as its
// final evaluation shows it.
type Stealth struct {
	Distfiles []StealthDistfile
	// Revbumped is true when the revision was bumped, since the source
	// changed; RevbumpProblem says why it could not be, when asked.
	Revbumped      bool
	RevbumpProblem string
	// DistSubdir is where mirrors now keep the new archive, as evaluated,
	// such as croc/10.2.4_1; empty when it was not set, and Problem says
	// why.
	DistSubdir string
	Problem    string
	// Unchanged is true where each archive's tree is the one MacPorts
	// shipped, file for file: only its packaging differs, and nothing is
	// revbumped. Differ are the files that differ where one doesn't, a
	// few; CompareProblem why the shipped archive couldn't be compared
	// (the docs dig's I19).
	Unchanged      bool
	Differ         []string
	CompareProblem string
}

// StealthDistfile is one archive's checksums before and after.
type StealthDistfile struct {
	Name     string
	Was, Now portfile.Checksum
}

// stealthUpdate finds a stealth update in a checksum refresh: an archive
// whose contents changed under the same name, in a Portfile the branch has
// not changed since its base. It bumps the selected port's revision, unless
// asked not to, and sets dist_subdir after the checksums: following the
// revision when it was bumped, numbered when not. The edits are evaluated
// and held to changing that alone. Ones that would change more, such as
// the revision of a subport that inherits it, are left for the person, with
// the checksums refreshed.
func (s *Service) stealthUpdate(ctx context.Context, request Request, input *sourceInput, result *Result) error {
	asked := request.Stealth
	if asked == nil || len(result.Files) != 1 || len(result.Fidelity) == 0 || len(result.Downloads) == 0 || result.Prepared.Ports == nil {
		return nil
	}
	name := input.target.Name
	// A revision already bumped by hand since the base isn't bumped again,
	// and dist_subdir follows it.
	keep, byHand := asked.KeepRevision, false
	if slices.Contains(asked.Changed, input.target.Portfile) {
		current := result.Fidelity[0].Before.Ports[name]
		if asked.Base == nil || asked.Base.Version != current.Version {
			return nil
		}
		if current.Revision > asked.Base.Revision {
			keep, byHand = true, true
		}
	}
	was := distfetch.Declared(result.Fidelity[0].Before.Ports[name].Options["checksums"])
	found := &Stealth{}
	for _, download := range result.Downloads {
		before, ok := was[download.Name]
		if !ok && len(was) == 1 && len(result.Downloads) == 1 {
			before, ok = was[""]
		}
		if ok && distfetch.Differs(before, download.Checksum) {
			found.Distfiles = append(found.Distfiles, StealthDistfile{Name: download.Name, Was: before, Now: download.Checksum})
		}
	}
	if len(found.Distfiles) == 0 {
		return nil
	}
	result.Stealth = found
	previous := result.Prepared
	contents := result.Files[0].After
	// What changed decides the revision, where nobody already has: a
	// regenerated archive whose files are the ones MacPorts shipped, as
	// gitea-tea's renamed root and 24 bytes of tar metadata, isn't a new
	// source, while a re-roll could be a security event.
	if !keep {
		s.stealthContents(ctx, input, result, found)
		keep = found.Unchanged
	}
	if !keep {
		bumped, err := s.bumpRevision(ctx, input, contents, previous.Ports[name].Revision)
		if err != nil {
			found.RevbumpProblem = refusal.WordsOf(err)
		} else {
			contents, found.Revbumped = bumped, true
		}
	}
	if after, err := portfile.StealthDistSubdir(contents, found.Revbumped || byHand); err != nil {
		found.Problem = refusal.WordsOf(err)
	} else {
		contents = after
	}
	if bytes.Equal(contents, result.Files[0].After) {
		return nil
	}
	report, applied, err := s.correct(ctx, input, result, distSubdirCorrection(contents, name, found.Revbumped))
	if err != nil {
		return err
	}
	if !applied {
		found.Revbumped = false
		found.Problem = "the revision and dist_subdir it would set also change " + strings.Join(report.UnexpectedChanges, "; ") + ", so they are left for you"
		return nil
	}
	if found.Problem == "" {
		found.DistSubdir = result.Prepared.Ports[name].Options["dist_subdir"]
	}
	return nil
}

// stealthContents compares each archive whose checksum changed with the one
// MacPorts shipped under its name, fetched as shipped (distfetch's
// Shipped, from MacPorts' mirror under the port's dist_subdir, since
// upstream serves the new one), as archive.Tree reads them: Unchanged where
// every tree is the same, else the files that differ. Where an archive
// can't be had or read, it says why, and the caller revbumps as before.
func (s *Service) stealthContents(ctx context.Context, input *sourceInput, result *Result, found *Stealth) {
	directory, err := scratch.Dir("stealth-")
	if err != nil {
		found.CompareProblem = err.Error()
		return
	}
	defer os.RemoveAll(directory)
	store := s.Archives.Store(directory)
	name := input.target.Name
	shippedInfo, now := result.Fidelity[0].Before.Ports[name], result.Prepared.Ports[name]
	var differ []string
	for _, distfile := range found.Distfiles {
		i := slices.IndexFunc(result.Downloads, func(d distfetch.Download) bool { return d.Name == distfile.Name })
		if i < 0 {
			found.CompareProblem = distfile.Name + " wasn't fetched"
			return
		}
		fetched := result.Downloads[i]
		next, err := store.Fetch(ctx, now, distfetch.Source{Name: fetched.Name, URL: fetched.URL})
		if err == nil && distfetch.Differs(fetched.Checksum, next.Checksum) {
			err = fmt.Errorf("upstream's %s changed again while it was compared", fetched.Name)
		}
		if err != nil {
			found.CompareProblem = refusal.WordsOf(err)
			return
		}
		shipped, err := store.Shipped(ctx, shippedInfo, []macports.Distfile{{Name: fetched.Name, URLs: []string{fetched.URL}}})
		if err != nil {
			found.CompareProblem = "the archive MacPorts shipped couldn't be fetched: " + refusal.WordsOf(err)
			return
		}
		old, err := archive.Tree(ctx, shipped[0].Path)
		if err == nil {
			var tree map[string]string
			if tree, err = archive.Tree(ctx, next.Path); err == nil {
				differ = append(differ, archive.Differences(old, tree)...)
			}
		}
		if err != nil {
			found.CompareProblem = fmt.Sprintf("%s couldn't be read to compare: %s", fetched.Name, refusal.WordsOf(err))
			return
		}
	}
	found.Unchanged = len(differ) == 0
	found.Differ = differ
	progress.VerboseReport(ctx, "Compared %d archive(s) with what MacPorts shipped: %d file(s) differ", len(found.Distfiles), len(differ))
}

// dropStealthDistSubdir removes a stealth update's dist_subdir from a
// version update, where every archive the new version fetches has a name
// of its own. One whose name is the same in both versions, as yq's man
// page's is, still needs the line to keep its versions apart on the
// mirrors, so it stays (the hugo exercise's yq run); so does one whose
// current archives can't be told. The removal is evaluated and held to
// changing the selected port's dist_subdir alone; one that another port
// shares is left, and said.
func (s *Service) dropStealthDistSubdir(ctx context.Context, input *sourceInput, result *Result) error {
	if len(result.Files) != 1 || result.Prepared.Ports == nil {
		return nil
	}
	contents, removed, err := portfile.RemoveStealthDistSubdir(result.Files[0].After)
	if err != nil || !removed {
		return nil
	}
	if shared, err := sharedDistfile(ctx, input, result.Downloads); err != nil || shared != "" {
		return nil
	}
	report, applied, err := s.correct(ctx, input, result, distSubdirCorrection(contents, input.target.Name, false))
	if err != nil {
		return err
	}
	if !applied {
		progress.Report(ctx, "Warning: removing dist_subdir for %s would also change %s; left unchanged for manual review", input.target.Name, strings.Join(report.UnexpectedChanges, "; "))
		return nil
	}
	result.DistSubdirRemoved = true
	return nil
}

// sharedDistfile is an archive the new version fetches under a name the
// current version's has too, by MacPorts' own fetch plan for the Portfile
// as it stands; empty where every name changes.
func sharedDistfile(ctx context.Context, input *sourceInput, downloads []distfetch.Download) (string, error) {
	current, err := shippedPlan(ctx, input)
	if err != nil {
		return "", err
	}
	for _, download := range downloads {
		if slices.ContainsFunc(current, func(distfile macports.Distfile) bool { return distfile.Name == download.Name }) {
			return download.Name, nil
		}
	}
	return "", nil
}

// distSubdirCorrection is a correction allowed to change the selected
// port's dist_subdir alone, and its revision by one when revbumped: every
// other port, and every other option, as they were.
func distSubdirCorrection(contents []byte, selected string, revbumped bool) correction {
	c := correction{contents: contents, expected: []string{selected + ".dist_subdir"}}
	if revbumped {
		c.expected = append(c.expected, selected+".revision +1")
	}
	c.allowed = func(name string, prepared, corrected macports.PortInfo) macports.PortInfo {
		if name != selected {
			return prepared
		}
		prepared.Options["dist_subdir"] = corrected.Options["dist_subdir"]
		if revbumped {
			prepared.Revision++
		}
		return prepared
	}
	return c
}
