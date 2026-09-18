package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type managedCodex struct {
	SupervisorPID int    `json:"supervisorPid"`
	ChildPID      int    `json:"childPid"`
	Account       string `json:"account"`
	Project       string `json:"project"`
	CWD           string `json:"cwd"`
	SessionID     string `json:"sessionId,omitempty"`
	SessionPath   string `json:"sessionPath,omitempty"`
	Started       int64  `json:"started"`
}

type handoffRequest struct {
	Target  string `json:"target"`
	Project string `json:"project"`
}

type projectHandoffPlan struct {
	Project  string
	Source   string
	Target   string
	Sessions []codexSession
	Managed  []managedCodex
}

type projectHandoffResult struct {
	Project      string
	Source       string
	Target       string
	Sessions     int
	Copied       int
	Already      int
	Restarted    int
	NotRestarted int
	Global       bool
}

func (a *App) managedPath(pid int) string {
	return filepath.Join(a.Root, "running", strconv.Itoa(pid)+".json")
}

func (a *App) handoffPath(pid int) string {
	return filepath.Join(a.Root, "handoffs", strconv.Itoa(pid)+".json")
}

func resumeSessionID(args []string) string {
	for index, arg := range args {
		if arg != "resume" {
			continue
		}
		for _, candidate := range args[index+1:] {
			if strings.HasPrefix(candidate, "-") {
				continue
			}
			if sessionIDPattern.MatchString(candidate) {
				return strings.ToLower(candidate)
			}
			break
		}
	}
	return ""
}

func findSessionByID(sessions []codexSession, id string) (codexSession, bool) {
	for _, session := range sessions {
		if session.ID == id {
			return session, true
		}
	}
	return codexSession{}, false
}

func identifyManagedSession(home, project, cwd, knownID string, started time.Time) (codexSession, error) {
	sessions, err := sessionsInProject(home, project)
	if err != nil {
		return codexSession{}, err
	}
	return identifyManagedSessionFromList(sessions, cwd, knownID, started)
}

func identifyManagedSessionFromList(sessions []codexSession, cwd, knownID string, started time.Time) (codexSession, error) {
	if knownID != "" {
		if session, ok := findSessionByID(sessions, knownID); ok {
			return session, nil
		}
		return codexSession{}, fmt.Errorf("session %s was not found", knownID)
	}
	candidates := []codexSession{}
	for _, session := range sessions {
		if filepath.Clean(session.CWD) != filepath.Clean(cwd) {
			continue
		}
		delta := session.Created.Sub(started)
		if delta >= -5*time.Second && delta <= 30*time.Second {
			candidates = append(candidates, session)
		}
	}
	if len(candidates) != 1 {
		return codexSession{}, fmt.Errorf("could not identify one session for %s: found %d candidates", cwd, len(candidates))
	}
	return candidates[0], nil
}

func (a *App) writeManaged(record managedCodex) error {
	return writeJSON(a.managedPath(record.SupervisorPID), record)
}

func (a *App) readHandoff(pid int) (handoffRequest, bool, error) {
	path := a.handoffPath(pid)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return handoffRequest{}, false, nil
	}
	if err != nil {
		return handoffRequest{}, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return handoffRequest{}, false, errors.New("handoff request must be a regular file")
	}
	var request handoffRequest
	if err = readJSON(path, &request); err != nil {
		return handoffRequest{}, false, err
	}
	if err = validate(request.Target); err != nil {
		return handoffRequest{}, false, err
	}
	if !filepath.IsAbs(request.Project) {
		return handoffRequest{}, false, errors.New("handoff project path is invalid")
	}
	return request, true, nil
}

func (a *App) superviseCodex(initialAccount string, initialArgs []string) error {
	if os.Getenv("CODEX_HOME") != "" {
		return a.launch(initialAccount, initialArgs, true)
	}
	cwd := currentDirectory()
	project, err := projectRoot(cwd)
	if err != nil {
		return err
	}
	supervisorPID := os.Getpid()
	account := initialAccount
	args := append([]string{}, initialArgs...)
	knownID := resumeSessionID(args)
	defer os.Remove(a.managedPath(supervisorPID))
	defer os.Remove(a.handoffPath(supervisorPID))
	for {
		cli, commandArgs, env, commandErr := a.command(account, args, false)
		if commandErr != nil {
			return commandErr
		}
		cmd := processCommand(cli, commandArgs...)
		setProcessEnvironment(cmd, env)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		configureManagedProcess(cmd)
		started := time.Now()
		if commandErr = cmd.Start(); commandErr != nil {
			return commandErr
		}
		record := managedCodex{SupervisorPID: supervisorPID, ChildPID: cmd.Process.Pid, Account: account, Project: project, CWD: cwd, SessionID: knownID, Started: started.Unix()}
		if commandErr = a.writeManaged(record); commandErr != nil {
			_ = terminateManagedProcess(cmd, true)
			_ = cmd.Wait()
			return commandErr
		}
		waitErr := cmd.Wait()
		request, requested, requestErr := a.readHandoff(supervisorPID)
		if requestErr != nil {
			return requestErr
		}
		if !requested {
			return waitErr
		}
		_ = os.Remove(a.handoffPath(supervisorPID))
		if filepath.Clean(request.Project) != filepath.Clean(project) {
			return errors.New("handoff request does not match the managed project")
		}
		sourceHome, profileErr := a.require(account)
		if profileErr != nil {
			return profileErr
		}
		session, identifyErr := identifyManagedSession(sourceHome, project, cwd, knownID, started)
		if identifyErr != nil {
			return identifyErr
		}
		if _, _, copyErr := a.copySession(account, request.Target, session); copyErr != nil {
			return copyErr
		}
		fmt.Printf("\nXSwap resumed session %s with account %s.\n", session.ID, request.Target)
		account = request.Target
		knownID = session.ID
		args = []string{"resume", session.ID, "-C", cwd}
	}
}

func (a *App) managedForProject(project string) ([]managedCodex, error) {
	directory := filepath.Join(a.Root, "running")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	records := []managedCodex{}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		var record managedCodex
		if err = readJSON(path, &record); err != nil || record.SupervisorPID <= 0 || record.ChildPID <= 0 {
			continue
		}
		if !managedProcessAlive(record.SupervisorPID) || !managedProcessAlive(record.ChildPID) {
			_ = os.Remove(path)
			continue
		}
		if filepath.Clean(record.Project) == filepath.Clean(project) {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].SupervisorPID < records[j].SupervisorPID })
	return records, nil
}

func (a *App) projectHandoffSource(directory string) (projectHandoffPlan, error) {
	project, err := projectRoot(directory)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	if err = validateHandoffScope(project); err != nil {
		return projectHandoffPlan{}, err
	}
	source, err := a.accountForDirectory(directory)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	sourceHome, err := a.require(source)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	sessions, err := sessionsInProject(sourceHome, project)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	records, err := a.managedForProject(project)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	managed := []managedCodex{}
	for _, record := range records {
		if record.Account != source {
			continue
		}
		session, identifyErr := identifyManagedSessionFromList(sessions, record.CWD, record.SessionID, time.Unix(record.Started, 0))
		if identifyErr == nil {
			record.SessionID = session.ID
			record.SessionPath = session.Path
		}
		managed = append(managed, record)
	}
	return projectHandoffPlan{Project: project, Source: source, Sessions: sessions, Managed: managed}, nil
}

func selectedSessionMap(sessions []codexSession) map[string]bool {
	selected := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		selected[session.ID] = true
	}
	return selected
}

func filterHandoffPlan(plan projectHandoffPlan, selected map[string]bool) (projectHandoffPlan, error) {
	if selected == nil {
		return plan, nil
	}
	sessions := []codexSession{}
	for _, session := range plan.Sessions {
		if selected[session.ID] {
			sessions = append(sessions, session)
		}
	}
	if len(sessions) == 0 {
		return projectHandoffPlan{}, errors.New("select at least one conversation to continue")
	}
	managed := []managedCodex{}
	for _, record := range plan.Managed {
		if selected[record.SessionID] {
			managed = append(managed, record)
		}
	}
	plan.Sessions = sessions
	plan.Managed = managed
	return plan, nil
}

func (a *App) planSelectedProjectHandoff(directory, target string, selected map[string]bool) (projectHandoffPlan, error) {
	if _, err := a.require(target); err != nil {
		return projectHandoffPlan{}, err
	}
	settings, err := a.settings()
	if err != nil {
		return projectHandoffPlan{}, err
	}
	if disabled(settings, target) {
		return projectHandoffPlan{}, fmt.Errorf("account %q is disabled", target)
	}
	plan, err := a.projectHandoffSource(directory)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	if plan.Source == target {
		return projectHandoffPlan{}, errors.New("project already uses the selected account")
	}
	plan.Target = target
	return filterHandoffPlan(plan, selected)
}

func (a *App) planProjectHandoff(directory, target string) (projectHandoffPlan, error) {
	return a.planSelectedProjectHandoff(directory, target, nil)
}

func (a *App) requestProjectHandoff(plan projectHandoffPlan) (projectHandoffResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := a.lock(ctx, filepath.Join("locks", "project-handoff.lock"))
	if err != nil {
		return projectHandoffResult{}, err
	}
	defer unlock()
	selected := selectedSessionMap(plan.Sessions)
	current, err := a.planSelectedProjectHandoff(plan.Project, plan.Target, selected)
	if err != nil {
		return projectHandoffResult{}, err
	}
	if current.Source != plan.Source {
		return projectHandoffResult{}, errors.New("project account changed after confirmation; review the switch again")
	}
	plan = current
	result := projectHandoffResult{Project: plan.Project, Source: plan.Source, Target: plan.Target, Sessions: len(plan.Sessions), Global: isHomeScope(plan.Project)}
	selectedAccounts, err := a.managedForProject(plan.Project)
	if err != nil {
		return result, err
	}
	for _, record := range selectedAccounts {
		if record.Account == plan.Target {
			return result, fmt.Errorf("destination account %q already has a managed Codex process in this project", plan.Target)
		}
	}
	// Prepare and index every selected conversation before changing the project
	// account or stopping a terminal. Active source rollouts can append after
	// this snapshot; their supervisors safely fast-forward them once stopped.
	changed := map[string]bool{}
	for _, session := range plan.Sessions {
		_, copied, copyErr := a.copySession(plan.Source, plan.Target, session)
		if copyErr != nil {
			return result, copyErr
		}
		changed[session.ID] = copied
	}
	written := []string{}
	for _, record := range plan.Managed {
		request := handoffRequest{Target: plan.Target, Project: plan.Project}
		path := a.handoffPath(record.SupervisorPID)
		if err := writeJSON(path, request); err != nil {
			for _, pending := range written {
				_ = os.Remove(pending)
			}
			return result, err
		}
		written = append(written, path)
	}
	if result.Global {
		err = a.selectAccount(plan.Target)
	} else {
		_, err = a.pinProject(plan.Project, plan.Target)
	}
	if err != nil {
		for _, pending := range written {
			_ = os.Remove(pending)
		}
		return result, err
	}
	for index, record := range plan.Managed {
		if err = stopManagedProcess(record.ChildPID); err != nil {
			for _, pending := range written[index:] {
				_ = os.Remove(pending)
			}
			return result, fmt.Errorf("stop managed Codex process %d: %w", record.ChildPID, err)
		}
	}

	deadline := time.Now().Add(20 * time.Second)
	for len(plan.Managed) > 0 && time.Now().Before(deadline) {
		current, listErr := a.managedForProject(plan.Project)
		if listErr != nil {
			return result, listErr
		}
		result.Restarted = 0
		restarted := map[int]bool{}
		for _, record := range current {
			if record.Account == plan.Target {
				restarted[record.SupervisorPID] = true
			}
		}
		finished := 0
		for _, original := range plan.Managed {
			if restarted[original.SupervisorPID] {
				result.Restarted++
				finished++
			} else if !managedProcessAlive(original.SupervisorPID) {
				finished++
			}
		}
		if finished >= len(plan.Managed) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	result.NotRestarted = len(plan.Managed) - result.Restarted

	// Every source writer managed by XSwap has stopped or resumed by this point,
	// so copying the project history cannot race with an append to its rollout.
	sourceHome, err := a.require(plan.Source)
	if err != nil {
		return result, err
	}
	sessions, err := sessionsInProject(sourceHome, plan.Project)
	if err != nil {
		return result, err
	}
	for _, session := range sessions {
		if !selected[session.ID] {
			continue
		}
		_, copied, copyErr := a.copySession(plan.Source, plan.Target, session)
		if copyErr != nil {
			return result, copyErr
		}
		if copied {
			changed[session.ID] = true
		}
	}
	for _, session := range plan.Sessions {
		if changed[session.ID] {
			result.Copied++
		} else {
			result.Already++
		}
	}
	return result, nil
}
