package portedit

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
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
	was := archives.Declared(result.Fidelity[0].Before.Ports[name].Options["checksums"])
	found := &Stealth{}
	for _, download := range result.Downloads {
		before, ok := was[download.Name]
		if !ok && len(was) == 1 && len(result.Downloads) == 1 {
			before, ok = was[""]
		}
		if ok && archives.Differs(before, download.Checksum) {
			found.Distfiles = append(found.Distfiles, StealthDistfile{Name: download.Name, Was: before, Now: download.Checksum})
		}
	}
	if len(found.Distfiles) == 0 {
		return nil
	}
	result.Stealth = found
	previous := result.Prepared
	contents := result.Files[0].After
	if !keep {
		bumped, err := portfile.BumpRevision(contents, input.target.Subport, previous.Ports[name].Revision)
		if err != nil {
			found.RevbumpProblem = unsupportedReason(err)
		} else {
			contents, found.Revbumped = bumped, true
		}
	}
	if after, _, err := portfile.StealthDistSubdir(contents, found.Revbumped || byHand); err != nil {
		found.Problem = unsupportedReason(err)
	} else {
		contents = after
	}
	if bytes.Equal(contents, result.Files[0].After) {
		return nil
	}
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return err
	}
	report := distSubdirReport(previous, evaluated.after, name, found.Revbumped)
	if len(report.UnexpectedChanges) > 0 {
		found.Revbumped = false
		found.Problem = "the revision and dist_subdir it would set also change " + strings.Join(report.UnexpectedChanges, "; ") + ", so they are left for you"
		return nil
	}
	if found.Problem == "" {
		found.DistSubdir = evaluated.after.Ports[name].Options["dist_subdir"]
	}
	result.Files = []portfile.Edit{evaluated.edit}
	result.report(report)
	return nil
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
	previous := result.Prepared
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return err
	}
	report := distSubdirReport(previous, evaluated.after, input.target.Name, false)
	if len(report.UnexpectedChanges) > 0 {
		progress.Report(ctx, "Warning: removing %s's stealth dist_subdir would also change %s; it is left for you", input.target.Name, strings.Join(report.UnexpectedChanges, "; "))
		return nil
	}
	result.Files = []portfile.Edit{evaluated.edit}
	result.report(report)
	result.DistSubdirRemoved = true
	return nil
}

// sharedDistfile is an archive the new version fetches under a name the
// current version's has too, by MacPorts' own fetch plan for the Portfile
// as it stands; empty where every name changes.
func sharedDistfile(ctx context.Context, input *sourceInput, downloads []archives.Download) (string, error) {
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

// distSubdirReport expects only the selected port's dist_subdir to change,
// and its revision to advance by one when revbumped: every other port, and
// every other option, as they were.
func distSubdirReport(before, after macports.Snapshot, selected string, revbumped bool) Fidelity {
	report := Fidelity{Before: before, After: after, ExpectedChanges: []string{selected + ".dist_subdir"}, UnexpectedChanges: []string{}}
	if revbumped {
		report.ExpectedChanges = append(report.ExpectedChanges, selected+".revision +1")
	}
	for name := range after.Ports {
		if _, ok := before.Ports[name]; !ok {
			report.UnexpectedChanges = append(report.UnexpectedChanges, name+": port set changed")
		}
	}
	for name, old := range before.Ports {
		next, ok := after.Ports[name]
		if !ok {
			report.UnexpectedChanges = append(report.UnexpectedChanges, name+": port set changed")
			continue
		}
		wanted, expected := old.Revision, old
		if name == selected {
			if revbumped {
				wanted++
			}
			expected.Options = maps.Clone(old.Options)
			if expected.Options == nil {
				expected.Options = map[string]string{}
			}
			expected.Options["dist_subdir"] = next.Options["dist_subdir"]
		}
		if next.Revision != wanted {
			report.UnexpectedChanges = append(report.UnexpectedChanges, fmt.Sprintf("%s.revision: expected %d, got %d", name, wanted, next.Revision))
		}
		report.UnexpectedChanges = append(report.UnexpectedChanges, fidelity.Compare(name, fidelity.ComparablePort(expected, before.Root), fidelity.ComparablePort(next, after.Root))...)
	}
	slices.Sort(report.UnexpectedChanges)
	return report
}

// unsupportedReason is an unsupported edit's reason, without the sentinel's
// own words.
func unsupportedReason(err error) string {
	return strings.TrimPrefix(err.Error(), portfile.ErrUnsupported.Error()+": ")
}
