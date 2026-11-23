package engine

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// RequestSettings are the settings a request falls back on where no flag
// was given and nothing is kept: the source policy, read when the request
// is built (docs/designs/preparation-and-request.md, "The request").
type RequestSettings struct {
	Source       string
	KeepHTTP     bool
	KeepModeline bool
}

// Asking is what the resolver takes: who asks, the flags as given, each
// with whether it was (model.Flag), the ports discovery found and the
// version named, the settings, and the branch whose kept request it goes
// on with, where there is one.
type Asking struct {
	// Origin is update, bump, ship or serve.
	Origin string
	// Asked holds the flags; its Targets, Outcomes and ID are ignored.
	Asked model.ChangeRequest
	// Ports are the targets discovery found; none goes on with the kept
	// targets. Named is the version named, for a lone port.
	Ports    []string
	Named    string
	Settings RequestSettings
	// Branch is the branch the request is for; its Request is the one
	// kept, and its Closes the tickets it closes already.
	Branch *model.Branch
	// Title and Note are the pull request's title and note as given, the
	// branch's own fields, saved with the request's first save so a ship
	// killed before its commit keeps them (#195, AL); an empty Note given
	// clears it (--no-note).
	Title, Note model.Flag[string]
}

// Unresolved is a fact the request carries as asked, resolved at its
// stage: what it is, and when.
type Unresolved struct {
	What, Why string
}

// Resolved is the resolver's answer: the request, the source policy it
// resolved, the outcome of each action it could judge already, each fact
// not resolved yet with its reason, and what it says of the kept request
// it went on with.
type Resolved struct {
	Request    model.ChangeRequest
	Policy     RequestSettings
	Outcomes   []model.ActionOutcome
	Unresolved []Unresolved
	Said       []string
}

// tickets are what --closes takes: a Trac ticket's number.
var tickets = regexp.MustCompile(`^[1-9][0-9]*$`)

// testPolicies are --tests' three values.
var testPolicies = []string{"declared", "required", "skip"}

// ResolveRequest builds the one request update, bump, ship and serve go
// on (#195): the flags given replace what is kept, and are said; a flag
// left out leaves it; the source policy comes from the settings where
// neither gives it, and the settings are never kept. It refuses, before any edit, what it can know is
// wrong: --except with no --revbump-dependents, a ticket that isn't a
// number, a version named for several ports, and a --tests that isn't one
// of the three. An outcome judged by an ask that changed is removed, so
// the action is owed again. No caller reaches it until #195's step 5.
func ResolveRequest(asking Asking) (Resolved, error) {
	asked := asking.Asked
	if err := refuseAsked(asking); err != nil {
		return Resolved{}, err
	}
	var kept model.ChangeRequest
	hasKept := asking.Branch != nil && asking.Branch.Request != nil
	if hasKept {
		kept = *asking.Branch.Request
		kept.Targets = slices.Clone(kept.Targets)
		for i := range kept.Targets {
			kept.Targets[i].Outcomes = slices.Clone(kept.Targets[i].Outcomes)
		}
		kept.Outcomes = slices.Clone(kept.Outcomes)
	}
	resolved := Resolved{}
	request := kept
	request.Origin = asking.Origin
	changed := map[string]bool{}
	if asking.Origin == "serve" {
		// serve's request holds its origin and targets: each of its
		// actions reads the process's configuration then ("serve").
		request.Shared, request.Source, request.Actions, request.To, request.Check, request.Publish = model.Flag[bool]{}, model.SourceAsk{}, model.ActionsAsk{}, model.Flag[string]{}, model.CheckAsk{}, model.PublishAsk{}
	} else {
		say := func(flag string, given, before any) {
			if hasKept {
				resolved.Said = append(resolved.Said, fmt.Sprintf("%s %v replaces the kept %v", flag, words(given), words(before)))
			}
		}
		was := shapingValues(request)
		replace(&request.Shared, asked.Shared, "--shared-release", "Shared", changed, say)
		replace(&request.Source.Source, asked.Source.Source, "--source", "Source.Source", changed, say)
		replace(&request.Source.KeepHTTP, asked.Source.KeepHTTP, "--keep-http", "Source.KeepHTTP", changed, say)
		replace(&request.Source.KeepOldChecksums, asked.Source.KeepOldChecksums, "--keep-old-checksums", "Source.KeepOldChecksums", changed, say)
		replace(&request.Source.KeepModeline, asked.Source.KeepModeline, "--keep-modeline", "Source.KeepModeline", changed, say)
		replace(&request.Source.FollowMoves, asked.Source.FollowMoves, "--follow-moves", "Source.FollowMoves", changed, say)
		replace(&request.Actions.WithObsolete, asked.Actions.WithObsolete, "--with-obsolete", "Actions.WithObsolete", changed, say)
		replace(&request.Actions.RevbumpDependents, asked.Actions.RevbumpDependents, "--revbump-dependents", "Actions.RevbumpDependents", changed, say)
		replace(&request.Actions.Except, asked.Actions.Except, "--except", "Actions.Except", changed, say)
		replace(&request.Actions.Closes, asked.Actions.Closes, "--closes", "Actions.Closes", changed, say)
		// The stage is ship's: bump is --to submit, given, and update
		// neither reads nor changes a kept one.
		switch asking.Origin {
		case "bump":
			replace(&request.To, model.Set(string(ShipSubmit)), "--to", "To", changed, say)
		case "ship":
			replace(&request.To, asked.To, "--to", "To", changed, say)
		}
		// --on and --build are one choice, the place: either given
		// replaces the other kept.
		if asked.Check.On.Given || asked.Check.Build.Given {
			before := placeWords(request.Check)
			request.Check.On, request.Check.Build = asked.Check.On, asked.Check.Build
			if after := placeWords(request.Check); before != after {
				changed["Check.On"], changed["Check.Build"] = true, true
				if hasKept && before != "" {
					resolved.Said = append(resolved.Said, fmt.Sprintf("%s replaces the kept %s", after, before))
				}
			}
		}
		replace(&request.Check.Tests, asked.Check.Tests, "--tests", "Check.Tests", changed, say)
		replace(&request.Publish.Draft, asked.Publish.Draft, "--draft", "Publish.Draft", changed, say)
		replace(&request.Publish.Type, asked.Publish.Type, "--type", "Publish.Type", changed, say)
		replace(&request.Publish.SkipNotification, asked.Publish.SkipNotification, "--skip-notification", "Publish.SkipNotification", changed, say)
		replace(&request.Publish.NoCheck, asked.Publish.NoCheck, "--no-check", "Publish.NoCheck", changed, say)
		replace(&request.Publish.TestedBinaries, asked.Publish.TestedBinaries, "--tested-binaries", "Publish.TestedBinaries", changed, say)
		replace(&request.Publish.TestedVariants, asked.Publish.TestedVariants, "--tested-variants", "Publish.TestedVariants", changed, say)
		shaped := shapedAfterEdit(&request, was, changed)
		removeJudged(&request, changed, shaped)
		if len(changed) > 0 {
			request.ID = ""
		}
	}
	// The source policy is resolved from the settings where no flag gives
	// it, given or kept, and is not kept as an ask: a later command reads
	// its own settings. An outcome a setting judged is judged again where
	// the setting gives another value now, on the same ask.
	resolved.Policy = policyOf(request, asking.Settings)
	rejudge(&request, resolved.Policy)
	targetsChanged := resolveTargets(&request, asking, &resolved)
	if targetsChanged {
		request.Outcomes = slices.DeleteFunc(request.Outcomes, func(o model.ActionOutcome) bool { return o.Action == "dependents" })
	}
	// A tested statement pending is for the work it was given for: where
	// what the files will be changes, a target or a release, a source ask
	// or a side action, one not given again now is dropped, and said
	// (#195, "What is kept").
	workChanged := targetsChanged
	for ask := range changed {
		workChanged = workChanged || ask == "Shared" || strings.HasPrefix(ask, "Source.") || strings.HasPrefix(ask, "Actions.")
	}
	if workChanged {
		resolved.Said = append(resolved.Said, dropPending(&request, asked.Publish)...)
	}
	if request.ID == "" || targetsChanged {
		request.ID = store.NewID("rq")
	}
	resolved.Request = request
	resolved.Outcomes = judgedNow(request, asking.Branch)
	resolved.Unresolved = unresolvedOf(request)
	return resolved, nil
}

// refuseAsked refuses what the flags ask that can't be, before any edit,
// in the words the commands refuse it today.
func refuseAsked(asking Asking) error {
	asked := asking.Asked
	if len(asked.Actions.Except.Value) > 0 && !asked.Actions.RevbumpDependents.Value {
		return errors.New("--except takes a port out of --revbump-dependents; add --revbump-dependents")
	}
	for _, ticket := range asked.Actions.Closes.Value {
		if !tickets.MatchString(ticket) {
			return fmt.Errorf("--closes %s: a Trac ticket is its number, as 71234", ticket)
		}
	}
	if asking.Named != "" && len(asking.Ports) > 1 {
		return fmt.Errorf("a version goes with a lone port, as update %s %s", asking.Ports[0], asking.Named)
	}
	if tests := asked.Check.Tests; tests.Given && !slices.Contains(testPolicies, tests.Value) {
		return fmt.Errorf("--tests %q is not declared, required, or skip", tests.Value)
	}
	if asked.Check.On.Given && asked.Check.Build.Given {
		return errors.New("if any flags in the group [build on] are set none of the others can be; [build on] were all set")
	}
	return nil
}

// replace puts a flag given in place of the kept field, says it where it
// differs from one kept, and marks the field changed where its value does
// or where nothing was kept: an outcome judged by a setting is judged
// again, since what it was judged by isn't kept.
func replace[T any](field *model.Flag[T], given model.Flag[T], flag, name string, changed map[string]bool, say func(string, any, any)) {
	if !given.Given {
		return
	}
	differs := fmt.Sprint(field.Value) != fmt.Sprint(given.Value)
	if differs || !field.Given {
		changed[name] = true
	}
	if field.Given && differs {
		say(flag, given.Value, field.Value)
	}
	*field = given
}

// judgedBy are the asks each outcome was judged by, beside the release
// selected (docs/designs/preparation-and-request.md, "Each action has its
// own outcome"): an outcome is removed when any of them changes.
var judgedBy = map[string][]string{
	"version":    {"Shared", "Source.KeepHTTP", "Source.KeepOldChecksums", "Source.KeepModeline"},
	"source":     {"Source.Source", "Shared"},
	"moved":      {"Source.FollowMoves"},
	"stub":       {"Actions.WithObsolete"},
	"dependents": {"Actions.RevbumpDependents", "Actions.Except"},
	"ticket":     {"Actions.Closes"},
}

// removeJudged makes each action an ask it was judged by changed owed
// again, on the targets and on the request.
func removeJudged(request *model.ChangeRequest, changed map[string]bool, shaped map[string]bool) {
	var stale []string
	for action, asks := range judgedBy {
		if slices.ContainsFunc(asks, func(ask string) bool { return changed[ask] }) {
			stale = append(stale, action)
		}
	}
	request.Outcomes = slices.DeleteFunc(request.Outcomes, func(o model.ActionOutcome) bool { return slices.Contains(stale, o.Action) })
	if len(request.Outcomes) == 0 {
		request.Outcomes = nil
	}
	for i := range request.Targets {
		for _, action := range stale {
			// A version the shaping rule decided keeps what it decided.
			if action == "version" && shaped[request.Targets[i].Port] {
				continue
			}
			request.Targets[i].Unconclude(action)
		}
	}
}

// shaping are the asks that shape a version's edit as it is made, which
// nothing applies to a version already written (#195, AW).
var shaping = []struct{ ask, flag string }{
	{"Source.KeepHTTP", "--keep-http"}, {"Source.KeepOldChecksums", "--keep-old-checksums"},
	{"Source.KeepModeline", "--keep-modeline"}, {"Shared", "--shared-release"},
}

// shapingValues are the shaping asks' values in a request, as compared.
func shapingValues(request model.ChangeRequest) map[string]string {
	return map[string]string{
		"Source.KeepHTTP":         fmt.Sprint(request.Source.KeepHTTP.Value),
		"Source.KeepOldChecksums": fmt.Sprint(request.Source.KeepOldChecksums.Value),
		"Source.KeepModeline":     fmt.Sprint(request.Source.KeepModeline.Value),
		"Shared":                  fmt.Sprint(request.Shared.Value),
	}
}

// shapedAfterEdit is the rule for a shaping ask changed after its version's
// edit was written, and the one place it is decided: such a flag is
// refused by name, the refusal kept as the version's outcome and blocking,
// with the asks the edit was made with; given as it was again, the
// refusal goes and the version is done again. A flag changed before any
// edit is the new ask, left to removeJudged. Nothing here reads the
// Portfile: the design's "already so" is amended to "refused" pending the
// person's word, and its judges would go where the refusal is decided. It
// returns the targets whose version it decided.
func shapedAfterEdit(request *model.ChangeRequest, was map[string]string, changed map[string]bool) map[string]bool {
	now := shapingValues(*request)
	decided := map[string]bool{}
	for i := range request.Targets {
		target := &request.Targets[i]
		outcome, ok := target.Outcome("version")
		if !ok {
			continue
		}
		switch outcome.Result {
		case model.ActionDone:
			var flags []string
			applied := map[string]string{}
			for _, s := range shaping {
				if changed[s.ask] && was[s.ask] != now[s.ask] {
					flags = append(flags, s.flag)
					applied[s.ask] = was[s.ask]
				}
			}
			if len(flags) == 0 {
				continue
			}
			version := "its release"
			if target.Release != nil {
				version = target.Release.Version
			}
			concluded(target, model.ActionOutcome{Action: "version", Result: model.ActionRefused, AppliedWith: applied,
				Why: fmt.Sprintf("%s changed after %s's edit to %s, which no edit applies to a version already written; give %s as it was, or apply it by hand", strings.Join(flags, " and "), target.Port, version, map[bool]string{true: "it", false: "them"}[len(flags) == 1])})
			decided[target.Port] = true
		case model.ActionRefused:
			if len(outcome.AppliedWith) == 0 {
				continue
			}
			decided[target.Port] = true
			asWas := true
			for ask, value := range outcome.AppliedWith {
				asWas = asWas && now[ask] == value
			}
			if asWas {
				concluded(target, model.ActionOutcome{Action: "version", Result: model.ActionDone})
			}
		}
	}
	return decided
}

// settable are the asks a setting supplies where no flag gives them.
var settable = []string{"Source.Source", "Source.KeepHTTP", "Source.KeepModeline"}

// policyValue is an ask a setting can supply, as the policy gives it.
func policyValue(policy RequestSettings, ask string) string {
	switch ask {
	case "Source.Source":
		return policy.Source
	case "Source.KeepHTTP":
		return fmt.Sprint(policy.KeepHTTP)
	case "Source.KeepModeline":
		return fmt.Sprint(policy.KeepModeline)
	}
	return ""
}

// askGiven reports whether a flag gives a settable ask, given now or kept.
func askGiven(request model.ChangeRequest, ask string) bool {
	switch ask {
	case "Source.Source":
		return request.Source.Source.Given
	case "Source.KeepHTTP":
		return request.Source.KeepHTTP.Given
	case "Source.KeepModeline":
		return request.Source.KeepModeline.Given
	}
	return false
}

// JudgedBy is what an action's outcome records of what judged it: each
// input of the action a setting supplied, with the value the policy gave
// it. An input a flag gave is the ask's, and isn't recorded; none is nil.
func JudgedBy(request model.ChangeRequest, policy RequestSettings, action string) map[string]string {
	var judged map[string]string
	for _, ask := range judgedBy[action] {
		if !slices.Contains(settable, ask) || askGiven(request, ask) {
			continue
		}
		if judged == nil {
			judged = map[string]string{}
		}
		judged[ask] = policyValue(policy, ask)
	}
	return judged
}

// rejudge makes owed again each outcome a setting judged where the policy
// gives that input another value now. An outcome with no JudgedBy is
// judged by the ask alone.
func rejudge(request *model.ChangeRequest, policy RequestSettings) {
	moved := func(o model.ActionOutcome) bool {
		for ask, value := range o.JudgedBy {
			if policyValue(policy, ask) != value {
				return true
			}
		}
		return false
	}
	request.Outcomes = slices.DeleteFunc(request.Outcomes, moved)
	if len(request.Outcomes) == 0 {
		request.Outcomes = nil
	}
	for i := range request.Targets {
		for _, outcome := range slices.Clone(request.Targets[i].Outcomes) {
			if moved(outcome) {
				request.Targets[i].Unconclude(outcome.Action)
			}
		}
	}
}

// resolveTargets puts the ports discovery found beside the kept targets,
// each kept target going on with its release, and a version named
// replacing one; it reports whether the targets changed.
func resolveTargets(request *model.ChangeRequest, asking Asking, resolved *Resolved) bool {
	changed := false
	for _, port := range asking.Ports {
		i := slices.IndexFunc(request.Targets, func(t model.RequestTarget) bool { return t.Port == port })
		if i < 0 {
			request.Targets = append(request.Targets, model.RequestTarget{Port: port, Action: "update", Named: asking.Named})
			changed = true
			continue
		}
		target := &request.Targets[i]
		if asking.Named == "" || asking.Named == target.Named {
			continue
		}
		before := target.Named
		if target.Release != nil {
			before = target.Release.Version
		}
		if before != "" {
			resolved.Said = append(resolved.Said, fmt.Sprintf("%s %s replaces the kept %s", port, asking.Named, before))
		}
		// A version named is a new ask: the release, the version found
		// current and every outcome judged by the release go.
		*target = model.RequestTarget{Port: port, Action: target.Action, Named: asking.Named}
		changed = true
	}
	for _, target := range request.Targets {
		if target.Release != nil {
			resolved.Said = append(resolved.Said, fmt.Sprintf("%s goes on with %s, the release kept", target.Port, target.Release.Version))
		}
	}
	return changed
}

// judgedNow are the outcomes the resolver judges itself: a ticket the
// branch closes already.
func judgedNow(request model.ChangeRequest, branch *model.Branch) []model.ActionOutcome {
	closes := request.Actions.Closes.Value
	if len(closes) == 0 || branch == nil {
		return nil
	}
	for _, ticket := range closes {
		if !slices.Contains(branch.Closes, ticket) {
			return nil
		}
	}
	return []model.ActionOutcome{{Action: "ticket", Result: model.ActionSatisfied, Why: "the branch closes " + strings.Join(closes, ", ") + " already"}}
}

// unresolvedOf are the facts the request carries as asked: a target's
// release until it's selected, the place and the tests until the check is
// proposed, and the destination until the submission's plan.
func unresolvedOf(request model.ChangeRequest) []Unresolved {
	var facts []Unresolved
	for _, target := range request.Targets {
		if target.Named == "" && target.Release == nil && target.Current == "" {
			facts = append(facts, Unresolved{What: target.Port + "'s release", Why: "selected when the port is prepared"})
		}
	}
	if request.Origin == "update" {
		return facts
	}
	if !request.Check.On.Given && !request.Check.Build.Given {
		facts = append(facts, Unresolved{What: "where the check builds", Why: "decided when the check is queued"})
	}
	if !request.Check.Tests.Given {
		facts = append(facts, Unresolved{What: "the check's tests", Why: "decided when the check is queued"})
	}
	return append(facts, Unresolved{What: "the destination", Why: "decided at submit"})
}

// KeptForLater says the flags a later stage reads that a request keeps for it,
// as a stage that stops short says them: "Kept for the check: --on
// sequoia, --tests required." and "Kept for the submit: --draft, --type
// bugfix." The check's line goes where the command stopped before the
// check, the submit's where it stopped before the submit.
func KeptForLater(request model.ChangeRequest, stopped ShipStage) []string {
	var lines []string
	if stopped == ShipCommit {
		var check []string
		if place := placeWords(request.Check); place != "" {
			check = append(check, place)
		}
		if request.Check.Tests.Given {
			check = append(check, "--tests "+request.Check.Tests.Value)
		}
		if len(check) > 0 {
			lines = append(lines, "Kept for the check: "+strings.Join(check, ", ")+".")
		}
	}
	if stopped == ShipCommit || stopped == ShipCheck {
		var submit []string
		publish := request.Publish
		if publish.Draft.Given {
			submit = append(submit, flagWords("--draft", publish.Draft.Value))
		}
		if publish.Type.Given {
			submit = append(submit, "--type "+strings.Join(publish.Type.Value, ","))
		}
		if publish.SkipNotification.Given {
			submit = append(submit, flagWords("--skip-notification", publish.SkipNotification.Value))
		}
		if publish.NoCheck.Given {
			submit = append(submit, flagWords("--no-check", publish.NoCheck.Value))
		}
		if publish.TestedBinaries.Given {
			submit = append(submit, flagWords("--tested-binaries", publish.TestedBinaries.Value))
		}
		if publish.TestedVariants.Given {
			submit = append(submit, flagWords("--tested-variants", publish.TestedVariants.Value))
		}
		if len(submit) > 0 {
			lines = append(lines, "Kept for the submit: "+strings.Join(submit, ", ")+".")
		}
	}
	return lines
}

// placeWords is the place asked, as typed: --on's environments or
// --build's profile; empty where neither was.
func placeWords(check model.CheckAsk) string {
	switch {
	case check.On.Given:
		return "--on " + strings.Join(check.On.Value, ",")
	case check.Build.Given:
		return "--build " + check.Build.Value
	}
	return ""
}

// flagWords is a boolean flag as typed: --draft, or --draft=false.
func flagWords(flag string, value bool) string {
	if value {
		return flag
	}
	return flag + "=false"
}

// words is a flag's value as said: a list joined with commas.
func words(value any) string {
	if list, ok := value.([]string); ok {
		return strings.Join(list, ",")
	}
	return fmt.Sprint(value)
}

// UpdateRequestFor is the update stage's request for one target, built
// from the request: the version named or the release kept, and the edit's
// intent. The source policy, HTTP and modeline reach the preparer through
// the engine's options until #195's step 4.
func UpdateRequestFor(request model.ChangeRequest, target model.RequestTarget) UpdateRequest {
	update := UpdateRequest{Action: model.EditUpdate, Port: target.Port, Version: target.Named, Release: target.Release, fromRequest: true,
		EditIntent:   model.EditIntent{SharedRelease: request.Shared.Value, KeepOldChecksums: request.Source.KeepOldChecksums.Value},
		WithObsolete: request.Actions.WithObsolete.Value, FollowMoves: request.Source.FollowMoves.Value,
		Closes: request.Actions.Closes.Value, CompareUpstream: true, LookForOthers: true}
	if update.Version == "" && target.Release != nil {
		update.Version = target.Release.Version
	}
	return update
}

// WhereRequestFor is where the request asks its check to build: the
// place asked, else the settings of the moment, unattended or not.
func WhereRequestFor(request model.ChangeRequest, unattended bool) WhereRequest {
	return WhereRequest{On: request.Check.On.Value, Build: request.Check.Build.Value, Unattended: unattended}
}

// CheckRequestFor is the check stage's request of a branch, built from
// the request: the place and the tests asked, check.tests where none was.
func CheckRequestFor(request model.ChangeRequest, branch model.Branch, where Where, unattended bool) CheckRequest {
	return CheckRequest{Capture: CaptureRequest{Branch: branch}, Where: where, Tests: model.TestPolicy(request.Check.Tests.Value), Unattended: unattended}
}

// SubmitRequestFor is the submit stage's request, built from the request:
// the publication's asks, and the check it asked for as asked, so evidence
// that stands is judged against them (coversAsked); --no-check asks for
// none. The submission carries the request's ID, which its publication
// takes where the request is still that one (takeRequest).
func SubmitRequestFor(request model.ChangeRequest, branch model.Branch, unattended bool) SubmitRequest {
	publish := request.Publish
	submit := SubmitRequest{Branch: branch, RequestID: request.ID, Draft: publish.Draft.Value, Types: publish.Type.Value, SkipNotification: publish.SkipNotification.Value,
		NoCheck: publish.NoCheck.Value, PendingCheck: !publish.NoCheck.Value, Where: WhereRequestFor(request, unattended), Tests: model.TestPolicy(request.Check.Tests.Value)}
	if publish.TestedBinaries.Given {
		submit.TestedBinaries, submit.WithdrawBinaries = publish.TestedBinaries.Value, !publish.TestedBinaries.Value
	}
	if publish.TestedVariants.Given {
		submit.TestedVariants, submit.WithdrawVariants = publish.TestedVariants.Value, !publish.TestedVariants.Value
	}
	return submit
}

// AskUpdate is the update stage's request for one port, from what a
// command asked: the resolver refuses what it can before anything is
// opened, and the update resolves the request again under the branch's
// lock, from the request kept there then (Engine.update, keepRequest).
func AskUpdate(asking Asking, port string) (UpdateRequest, error) {
	asking.Ports = []string{port}
	resolved, err := ResolveRequest(asking)
	if err != nil {
		return UpdateRequest{}, err
	}
	i := slices.IndexFunc(resolved.Request.Targets, func(t model.RequestTarget) bool { return t.Port == port })
	update := UpdateRequestFor(resolved.Request, resolved.Request.Targets[i])
	update.Asking = &asking
	return update, nil
}

// checkedSubmitRequest is serve's submission of a branch whose check has
// run, built from the request kept on it: the publication's asks and the
// request's ID, with no check pending (#195).
func checkedSubmitRequest(branch model.Branch) SubmitRequest {
	var request model.ChangeRequest
	if branch.Request != nil {
		request = *branch.Request
	}
	submit := SubmitRequestFor(request, branch, true)
	submit.PendingCheck, submit.Where, submit.Tests = false, WhereRequest{}, ""
	return submit
}

// requestSettings are the settings the process was started with, which
// serve's request reads at each action (#195, "serve").
func (e *Engine) requestSettings() RequestSettings {
	return RequestSettings{Source: e.options.Source, KeepHTTP: e.options.KeepHTTP, KeepModeline: e.KeepModeline}
}

// askedUpdate is a driver's update of one port, through the resolver, as
// origin asks it with no flags: serve's, or ship's engine path.
func (e *Engine) askedUpdate(origin, port, version string, release *model.Release, unattended bool) UpdateRequest {
	// Nothing is asked, so nothing is refused.
	request, _ := AskUpdate(Asking{Origin: origin, Settings: e.requestSettings()}, port)
	request.Version, request.Release, request.Batch, request.Unattended, request.LookForOthers = version, release, true, unattended, false
	return request
}

// noRequest is the request of a check no request asks: recovery's and
// serve's, which read the process's settings (#195, "serve").
var noRequest model.ChangeRequest

// policyOf is the source policy a request resolves to under the settings:
// each ask a flag gave, given now or kept, else the setting.
func policyOf(request model.ChangeRequest, settings RequestSettings) RequestSettings {
	return RequestSettings{Source: request.Source.Source.Or(settings.Source),
		KeepHTTP: request.Source.KeepHTTP.Or(settings.KeepHTTP), KeepModeline: request.Source.KeepModeline.Or(settings.KeepModeline)}
}

// dropPending takes the tested statements pending in the request out,
// but one given now, and says each: a statement is of the files it was
// given for, and bound to nothing else.
func dropPending(request *model.ChangeRequest, givenNow model.PublishAsk) []string {
	var said []string
	if request.Publish.TestedBinaries.Given && !givenNow.TestedBinaries.Given {
		request.Publish.TestedBinaries = model.Flag[bool]{}
		said = append(said, "The tested statement --tested-binaries was given for other files and was dropped; give it again for these")
	}
	if request.Publish.TestedVariants.Given && !givenNow.TestedVariants.Given {
		request.Publish.TestedVariants = model.Flag[bool]{}
		said = append(said, "The tested statement --tested-variants was given for other files and was dropped; give it again for these")
	}
	return said
}
