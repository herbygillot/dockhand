package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// stateFile is one of serve's small files beside the database.
func stateFile(e *engine.Engine, name string) string {
	return filepath.Join(filepath.Dir(e.LogDirectory()), name)
}

// serveNow is the clock serve's daily work reads; tests set it.
var serveNow = time.Now

// outdatedFile is what serve's daily look at your ports found, for status.
type outdatedFile struct {
	CheckedAt time.Time `json:"checked_at"`
	Master    string    `json:"master"`
	Outdated  []string  `json:"outdated"`
}

// outdatedScanner looks for new releases of your ports once a day, at
// serve.outdated_at, and does what serve.for_outdated says.
type outdatedScanner struct {
	e        *engine.Engine
	out      io.Writer
	file     config.File
	notice   *notifier
	reported string
}

func (o *outdatedScanner) maybe(ctx context.Context) {
	now := serveNow()
	hour, minute := o.file.Serve.Time()
	due := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if now.Before(due) {
		return
	}
	stamp := stateFile(o.e, "outdated.stamp")
	if info, err := os.Stat(stamp); err == nil && !info.ModTime().Before(due) {
		return
	}
	if err := os.WriteFile(stamp, nil, 0o644); err != nil {
		return
	}
	_ = os.Chtimes(stamp, now, now)
	report := func(problem string) {
		if problem != o.reported {
			fmt.Fprintln(o.out, problem)
			o.reported = problem
		}
	}
	maintainers := o.file.Maintainers()
	if len(maintainers) == 0 {
		report(`serve: serve.for_outdated needs to know your ports: set maintainer = "{@you example.org:you}" in ~/.dockhand/config.toml`)
		return
	}
	found, err := o.e.Outdated(ctx, engine.OutdatedRequest{Maintainers: maintainers})
	if err != nil {
		report(fmt.Sprintf("serve: looking for new releases of your ports: %v", err))
		return
	}
	var names []string
	for _, port := range found.Ports {
		if port.Outdated {
			names = append(names, port.Port)
		}
	}
	record := outdatedFile{CheckedAt: now, Master: string(found.Master), Outdated: names}
	if data, err := json.Marshal(record); err == nil {
		_ = os.WriteFile(stateFile(o.e, "outdated.json"), data, 0o644)
	}
	if len(names) == 0 {
		fmt.Fprintln(o.out, "serve: none of your ports has a newer release")
		return
	}
	fmt.Fprintf(o.out, "serve: %s of yours %s newer releases: %s\n", plural(len(names), "port"), map[bool]string{true: "has", false: "have"}[len(names) == 1], strings.Join(names, ", "))
	mode := o.file.Serve.Mode()
	if mode == "list" {
		return
	}
	plan, err := o.e.PlanOutdated(ctx, found)
	if err != nil {
		report(fmt.Sprintf("serve: splitting the updates: %v", err))
		return
	}
	options := engine.PrepareOptions{Origin: model.OriginServe, Check: mode == "check", Tests: model.TestPolicy(o.file.Check.Tests)}
	if options.Check {
		if options.Environments, err = o.e.Environments(o.file.Check.On); err != nil {
			report(fmt.Sprintf("serve: serve.for_outdated = \"check\": %v", err))
			options.Check = false
		}
	}
	for _, done := range o.e.PrepareOutdated(ctx, plan, options) {
		if done.Problem != "" {
			fmt.Fprintf(o.out, "serve: %s: %s\n", done.Planned.Name, done.Problem)
			continue
		}
		line := fmt.Sprintf("serve: prepared %s: %s → %s", done.Branch.ShortName(), done.Update.Before, done.Update.After)
		if done.Run != nil {
			line += ", " + done.Run.Name() + " queued"
		}
		fmt.Fprintln(o.out, line)
		o.notice.post(done.Branch.ShortName(), strings.TrimPrefix(line, "serve: "))
	}
}

// readOutdated is what serve last found of your ports, if it looked.
func readOutdated(e *engine.Engine) (outdatedFile, bool) {
	var found outdatedFile
	data, err := os.ReadFile(stateFile(e, "outdated.json"))
	if err != nil || json.Unmarshal(data, &found) != nil {
		return found, false
	}
	return found, true
}

// submittedToday counts the pull requests serve opened on a day.
type submittedToday struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// passingSubmitter opens pull requests for the branches serve prepared
// whose checks passed, within Design v3 §11's guardrails, at most limit a
// day across restarts.
type passingSubmitter struct {
	e      *engine.Engine
	out    io.Writer
	limit  int
	notice *notifier
	last   time.Time
	held   map[model.BranchID]string
}

func (p *passingSubmitter) maybe(ctx context.Context) {
	if !p.last.IsZero() && time.Since(p.last) < serveRefresh {
		return
	}
	p.last = time.Now()
	if p.held == nil {
		p.held = map[model.BranchID]string{}
	}
	candidates, err := p.e.ServeCandidates(ctx)
	if err != nil {
		fmt.Fprintf(p.out, "serve: finding passing updates: %v\n", err)
		return
	}
	path := stateFile(p.e, "submitted.json")
	var today submittedToday
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &today)
	}
	if day := serveNow().Format("2006-01-02"); today.Day != day {
		today = submittedToday{Day: day}
	}
	for _, candidate := range candidates {
		name := candidate.Branch.ShortName()
		if len(candidate.Held) > 0 {
			if p.held[candidate.Branch.ID] != candidate.Held[0] {
				fmt.Fprintf(p.out, "serve: %s is held for a look: %s\n", name, candidate.Held[0])
				p.held[candidate.Branch.ID] = candidate.Held[0]
			}
			continue
		}
		if today.Count >= p.limit {
			fmt.Fprintf(p.out, "serve: %s waits for tomorrow; today's limit of %s is reached (serve.submit_limit)\n", name, plural(p.limit, "pull request"))
			return
		}
		submitted, err := p.e.SubmitForServe(ctx, candidate)
		if err != nil {
			fmt.Fprintf(p.out, "serve: submitting %s: %v\n", name, err)
			continue
		}
		today.Count++
		if data, err := json.Marshal(today); err == nil {
			_ = os.WriteFile(path, data, 0o644)
		}
		line := fmt.Sprintf("opened #%d for %s, which passed its check", submitted.PullRequest.Ref.Number, name)
		fmt.Fprintln(p.out, "serve: "+line)
		p.notice.post(name, line)
	}
}

// notifier posts macOS notifications, when serve.notify allows.
type notifier struct{ on bool }

// postNotification shows a notification; tests stand in for it.
var postNotification = func(title, text string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	quote := func(value string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"` }
	return exec.Command("osascript", "-e", "display notification "+quote(text)+" with title "+quote("dockhand · "+title)).Run()
}

func (n *notifier) post(title, text string) {
	if n != nil && n.on {
		_ = postNotification(title, text)
	}
}

// servingFile is what the leading serve says about itself for status and
// queue in other terminals.
type servingFile struct {
	PID           int  `json:"pid"`
	SubmitPassing bool `json:"submit_passing"`
}

func writeServing(e *engine.Engine, submitPassing bool) {
	if data, err := json.Marshal(servingFile{PID: os.Getpid(), SubmitPassing: submitPassing}); err == nil {
		_ = os.WriteFile(stateFile(e, "serving.json"), data, 0o644)
	}
}

func readServing(e *engine.Engine) (servingFile, bool) {
	var serving servingFile
	data, err := os.ReadFile(stateFile(e, "serving.json"))
	if err != nil || json.Unmarshal(data, &serving) != nil {
		return serving, false
	}
	return serving, true
}
