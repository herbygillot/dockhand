package engine

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/model"
)

// findings are what an update found and did, as its edit's result keeps
// them, field by field (docs/designs/preparation-and-request.md, "What is
// stored of engine.Update"): data, worded when shown (FindingWords). The
// rest of Update is on the edit or the branch, worked out from the
// records, or printed once and not read again.
func (u Update) findings() []model.Finding {
	var found []model.Finding
	add := func(kind, subject string, fixed bool, detail ...string) {
		found = append(found, model.Finding{Kind: kind, Subject: subject, Fixed: fixed, Detail: detail})
	}
	for _, patch := range patchcheck.Rejected(u.patches) {
		if !slices.Contains(u.PatchesDropped, patch.Name) {
			add(model.FindingPatchRejected, patch.Name, false, detailLines(patch.Detail)...)
		}
	}
	for _, patch := range u.patches {
		if !patch.Checked {
			add(model.FindingPatchUnchecked, patch.Name, false, detailLines(patch.Detail)...)
		}
	}
	if u.MissingWorksrcdir != "" {
		add(model.FindingWorksrcdirMissing, u.MissingWorksrcdir, false)
	}
	for _, instruction := range u.Instructions {
		add(model.FindingInstruction, "", false, instruction)
	}
	for _, url := range u.PlainHTTP {
		add(model.FindingPlainHTTP, url.URL, false, url.Option, url.HTTPS, strconv.FormatBool(url.Answers))
	}
	for _, other := range u.Others {
		add(model.FindingOtherPullRequest, other.URL, false, strconv.Itoa(other.Number), other.Title)
	}
	if u.ClosedBefore != "" {
		add(model.FindingClosedBefore, u.After.Version, false, u.ClosedBefore)
	}
	if u.CrossesMajor {
		add(model.FindingCrossesMajor, u.After.Version, false, u.Before.Version)
	}
	if u.LeavesStable {
		add(model.FindingLeavesStable, u.After.Version, false, u.Before.Version)
	}
	if u.Renamed != "" {
		add(model.FindingRenamed, u.Renamed, false)
	}
	if stealth := u.Stealth; stealth != nil {
		detail := []string{stealth.DistSubdir, strconv.FormatBool(stealth.Revbumped), stealth.RevbumpProblem, stealth.Problem}
		for _, distfile := range stealth.Distfiles {
			detail = append(detail, distfile.Name)
		}
		add(model.FindingStealth, u.Port, false, detail...)
	}
	for _, left := range u.ChecksumsLeft {
		add(model.FindingChecksumLeft, left.What, false, string(left.Kind), left.Reason)
	}
	for _, archive := range u.Differing {
		add(model.FindingChecksumDiffers, archive, false)
	}
	for _, dropped := range u.PatchesDropped {
		add(model.FindingPatchDropped, dropped, true)
	}
	for _, refreshed := range u.PatchesRefreshed {
		name, words, _ := strings.Cut(refreshed, ": ")
		add(model.FindingPatchRefreshed, name, true, words)
	}
	if u.Followed != "" {
		add(model.FindingFollowed, u.Followed, true)
	}
	for _, block := range u.Regenerated {
		detail := []string{strconv.Itoa(block.Count), strconv.Itoa(block.Changed), block.Unchecked}
		add(model.FindingRegenerated, block.Option, true, append(detail, block.Notices...)...)
	}
	for _, fixed := range u.Fixed {
		add(model.FindingLintFixed, "", true, fixed)
	}
	if u.DistSubdirRemoved {
		add(model.FindingDistSubdirRemoved, u.Port, true)
	}
	return found
}

// detailLines are a tool's output as its lines, kept as captured.
func detailLines(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// FindingWords say a recorded finding. A kind this dockhand doesn't know,
// one a later one recorded, is said by its name, subject and detail.
func FindingWords(finding model.Finding) string {
	detail := func(i int) string {
		if i < len(finding.Detail) {
			return finding.Detail[i]
		}
		return ""
	}
	switch finding.Kind {
	case model.FindingPatchRejected:
		return fmt.Sprintf("patch %s doesn't apply", finding.Subject)
	case model.FindingPatchUnchecked:
		return fmt.Sprintf("patch %s wasn't checked", finding.Subject)
	case model.FindingWorksrcdirMissing:
		return fmt.Sprintf("worksrcdir %s isn't in the new source", finding.Subject)
	case model.FindingInstruction:
		return "the Portfile says: " + detail(0)
	case model.FindingPlainHTTP:
		return fmt.Sprintf("%s is over plain HTTP: %s", detail(0), finding.Subject)
	case model.FindingOtherPullRequest:
		return fmt.Sprintf("another open pull request: #%s %s", detail(0), detail(1))
	case model.FindingClosedBefore:
		return detail(0)
	case model.FindingCrossesMajor:
		return fmt.Sprintf("a new major version: %s to %s", detail(0), finding.Subject)
	case model.FindingLeavesStable:
		return fmt.Sprintf("a prerelease after a stable version: %s to %s", detail(0), finding.Subject)
	case model.FindingRenamed:
		return "the repository is now " + finding.Subject
	case model.FindingStealth:
		return "a stealth update: the archive changed under the same name"
	case model.FindingChecksumLeft:
		return fmt.Sprintf("checksums left for %s: %s", finding.Subject, detail(1))
	case model.FindingChecksumDiffers:
		return fmt.Sprintf("%s isn't as the Portfile's checksums say", finding.Subject)
	case model.FindingPatchDropped:
		return fmt.Sprintf("dropped patch %s, which the new source holds", finding.Subject)
	case model.FindingPatchRefreshed:
		return fmt.Sprintf("refreshed patch %s: %s", finding.Subject, detail(0))
	case model.FindingFollowed:
		return "followed the repository to " + finding.Subject
	case model.FindingRegenerated:
		return fmt.Sprintf("regenerated %s", finding.Subject)
	case model.FindingLintFixed:
		return "fixed: " + detail(0)
	case model.FindingDistSubdirRemoved:
		return "removed the dist_subdir an earlier stealth update set"
	}
	words := finding.Kind
	if finding.Subject != "" {
		words += ": " + finding.Subject
	}
	if len(finding.Detail) > 0 {
		words += " (" + strings.Join(finding.Detail, "; ") + ")"
	}
	return words
}
