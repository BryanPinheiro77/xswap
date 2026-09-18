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
	Target    string `json:"target"`
	Project   string `json:"project"`
	SessionID string `json:"sessionId,omitempty"`
}

type projectHandoffPlan struct {
	Project   string
	Sources   []string
	Target    string
	Sessions  []codexSession
	Managed   []managedCodex
	Unmanaged []codexSession
}

type sessionProject struct {
	Root     string
	Accounts int
	Count    int
	Updated  time.Time
}

type projectHandoffResult struct {
	Project      string
	Sources      []string
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
	if request.SessionID != "" && !sessionIDPattern.MatchString(request.SessionID) {
		return handoffRequest{}, false, errors.New("handoff session id is invalid")
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
		if request.SessionID != "" {
			knownID = request.SessionID
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

func (a *App) sessionProjects() ([]sessionProject, error) {
	type discovered struct {
		accounts map[string]bool
		sessions map[string]codexSession
	}
	found := map[string]*discovered{}
	for _, name := range a.names() {
		home, err := a.require(name)
		if err != nil {
			return nil, err
		}
		sessions, err := sessionsInProfile(home)
		if err != nil {
			return nil, err
		}
		for _, session := range sessions {
			root, rootErr := projectRoot(session.CWD)
			if rootErr != nil || validateHandoffScope(root) != nil {
				continue
			}
			root = filepath.Clean(root)
			item := found[root]
			if item == nil {
				item = &discovered{accounts: map[string]bool{}, sessions: map[string]codexSession{}}
				found[root] = item
			}
			item.accounts[name] = true
			if existing, ok := item.sessions[session.ID]; !ok || session.Updated.After(existing.Updated) {
				session.Account = name
				item.sessions[session.ID] = session
			}
		}
	}
	projects := make([]sessionProject, 0, len(found))
	for root, item := range found {
		var updated time.Time
		for _, session := range item.sessions {
			if session.Updated.After(updated) {
				updated = session.Updated
			}
		}
		projects = append(projects, sessionProject{Root: root, Accounts: len(item.accounts), Count: len(item.sessions), Updated: updated})
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Updated.Equal(projects[j].Updated) {
			return projects[i].Root < projects[j].Root
		}
		return projects[i].Updated.After(projects[j].Updated)
	})
	return projects, nil
}

func compatibleSessionCopy(candidates []codexSession) (codexSession, error) {
	chosen := candidates[0]
	for _, candidate := range candidates[1:] {
		chosenPrefix, err := fileIsPrefix(chosen.Path, candidate.Path)
		if err != nil {
			return codexSession{}, err
		}
		candidatePrefix, err := fileIsPrefix(candidate.Path, chosen.Path)
		if err != nil {
			return codexSession{}, err
		}
		if !chosenPrefix && !candidatePrefix {
			return codexSession{}, fmt.Errorf("session %s has diverged between accounts", chosen.ID)
		}
		if chosenPrefix && (!candidatePrefix || candidate.Updated.After(chosen.Updated)) {
			chosen = candidate
		}
	}
	return chosen, nil
}

func (a *App) projectHandoffSource(directory string) (projectHandoffPlan, error) {
	project, err := projectRoot(directory)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	if err = validateHandoffScope(project); err != nil {
		return projectHandoffPlan{}, err
	}
	accountSessions := map[string][]codexSession{}
	accountHomes := map[string]string{}
	variants := map[string][]codexSession{}
	for _, name := range a.names() {
		home, profileErr := a.require(name)
		if profileErr != nil {
			return projectHandoffPlan{}, profileErr
		}
		accountHomes[name] = home
		sessions, sessionsErr := sessionsInProject(home, project)
		if sessionsErr != nil {
			return projectHandoffPlan{}, sessionsErr
		}
		for index := range sessions {
			sessions[index].Account = name
			variants[sessions[index].ID] = append(variants[sessions[index].ID], sessions[index])
		}
		accountSessions[name] = sessions
	}
	records, err := a.managedForProject(project)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	managedBySession := map[string][]managedCodex{}
	managedKeys := map[string]bool{}
	for index, record := range records {
		session, identifyErr := identifyManagedSessionFromList(accountSessions[record.Account], record.CWD, record.SessionID, time.Unix(record.Started, 0))
		if identifyErr == nil {
			record.SessionID = session.ID
			record.SessionPath = session.Path
			records[index] = record
			managedBySession[session.ID] = append(managedBySession[session.ID], record)
			managedKeys[record.Account+"\x00"+session.ID] = true
		}
	}
	sessions := make([]codexSession, 0, len(variants))
	for id, candidates := range variants {
		managed := managedBySession[id]
		if len(managed) > 1 {
			return projectHandoffPlan{}, fmt.Errorf("conversation %s is open in multiple accounts", id)
		}
		chosen, compatibleErr := compatibleSessionCopy(candidates)
		if compatibleErr != nil {
			return projectHandoffPlan{}, compatibleErr
		}
		if len(managed) == 1 {
			for _, candidate := range candidates {
				if candidate.Account == managed[0].Account {
					managedIsCurrent, prefixErr := fileIsPrefix(chosen.Path, candidate.Path)
					if prefixErr != nil {
						return projectHandoffPlan{}, prefixErr
					}
					if !managedIsCurrent {
						return projectHandoffPlan{}, fmt.Errorf("open conversation %s in account %s is behind another account copy", id, candidate.Account)
					}
					chosen = candidate
					break
				}
			}
		}
		sessions = append(sessions, chosen)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Updated.After(sessions[j].Updated) })
	unmanaged := []codexSession{}
	for _, candidates := range variants {
		for _, session := range candidates {
			if managedKeys[session.Account+"\x00"+session.ID] {
				continue
			}
			open, openErr := a.sessionIsOpen(accountHomes[session.Account], session.ID)
			if openErr != nil {
				return projectHandoffPlan{}, fmt.Errorf("inspect session %s in account %s: %w", session.ID, session.Account, openErr)
			}
			if open {
				unmanaged = append(unmanaged, session)
			}
		}
	}
	return projectHandoffPlan{Project: project, Sources: sessionAccounts(sessions), Sessions: sessions, Managed: records, Unmanaged: unmanaged}, nil
}

func sessionAccounts(sessions []codexSession) []string {
	found := map[string]bool{}
	for _, session := range sessions {
		if session.Account != "" {
			found[session.Account] = true
		}
	}
	accounts := make([]string, 0, len(found))
	for name := range found {
		accounts = append(accounts, name)
	}
	sort.Strings(accounts)
	return accounts
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
	selectedByCWD := map[string][]codexSession{}
	for _, session := range sessions {
		cwd := filepath.Clean(session.CWD)
		selectedByCWD[cwd] = append(selectedByCWD[cwd], session)
	}
	unknownByScope := map[string]int{}
	for _, record := range plan.Managed {
		if record.SessionID == "" {
			unknownByScope[record.Account+"\x00"+filepath.Clean(record.CWD)]++
		}
	}
	managed := []managedCodex{}
	claimed := map[string]bool{}
	for _, record := range plan.Managed {
		session, selectedSession := findSessionByID(sessions, record.SessionID)
		if selectedSession && session.Account == record.Account {
			managed = append(managed, record)
			claimed[record.SessionID] = true
		}
	}
	for _, record := range plan.Managed {
		if record.SessionID != "" {
			continue
		}
		cwd := filepath.Clean(record.CWD)
		candidates := []codexSession{}
		for _, session := range selectedByCWD[cwd] {
			if session.Account == record.Account && !claimed[session.ID] {
				candidates = append(candidates, session)
			}
		}
		if unknownByScope[record.Account+"\x00"+cwd] == 1 && len(candidates) == 1 {
			record.SessionID = candidates[0].ID
			record.SessionPath = candidates[0].Path
			managed = append(managed, record)
			claimed[record.SessionID] = true
		}
	}
	plan.Sessions = sessions
	plan.Sources = sessionAccounts(sessions)
	plan.Managed = managed
	unmanaged := []codexSession{}
	for _, session := range plan.Unmanaged {
		if selected[session.ID] {
			unmanaged = append(unmanaged, session)
		}
	}
	plan.Unmanaged = unmanaged
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
	plan.Target = target
	plan, err = filterHandoffPlan(plan, selected)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	moves := false
	for _, session := range plan.Sessions {
		if session.Account != target {
			moves = true
			break
		}
	}
	if !moves {
		return projectHandoffPlan{}, errors.New("selected conversations already use the destination account")
	}
	managed := plan.Managed[:0]
	for _, record := range plan.Managed {
		if record.Account != target {
			managed = append(managed, record)
		}
	}
	plan.Managed = managed
	unmanaged := plan.Unmanaged[:0]
	for _, session := range plan.Unmanaged {
		chosen, ok := findSessionByID(plan.Sessions, session.ID)
		if ok && chosen.Account != target {
			unmanaged = append(unmanaged, session)
		}
	}
	plan.Unmanaged = unmanaged
	return plan, nil
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
	for _, session := range plan.Sessions {
		currentSession, ok := findSessionByID(current.Sessions, session.ID)
		if !ok || currentSession.Account != session.Account {
			return projectHandoffResult{}, errors.New("conversation sources changed after confirmation; review the handoff again")
		}
	}
	plan = current
	transferSources := []string{}
	for _, source := range plan.Sources {
		if source != plan.Target {
			transferSources = append(transferSources, source)
		}
	}
	result := projectHandoffResult{Project: plan.Project, Sources: transferSources, Target: plan.Target, Sessions: len(plan.Sessions), Global: isHomeScope(plan.Project)}
	if len(plan.Unmanaged) > 0 {
		titles := make([]string, 0, len(plan.Unmanaged))
		for _, session := range plan.Unmanaged {
			titles = append(titles, sessionTitle(session, plan.Project)+" ("+session.Account+")")
		}
		return result, fmt.Errorf("open sessions are outside XSwap supervision: %s; close them, reopen with codex resume, and try again", strings.Join(titles, "; "))
	}
	// Prepare every selected conversation before changing the project account or
	// stopping a terminal. Active source rollouts can append after this snapshot;
	// their supervisors safely fast-forward them once stopped.
	changed := map[string]bool{}
	for _, session := range plan.Sessions {
		if session.Account == plan.Target {
			continue
		}
		_, copied, copyErr := a.copySession(session.Account, plan.Target, session)
		if copyErr != nil {
			return result, copyErr
		}
		changed[session.ID] = copied
	}
	written := []string{}
	for _, record := range plan.Managed {
		request := handoffRequest{Target: plan.Target, Project: plan.Project, SessionID: record.SessionID}
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
	refreshed := map[string][]codexSession{}
	for _, session := range plan.Sessions {
		if session.Account == plan.Target {
			continue
		}
		if _, ok := refreshed[session.Account]; !ok {
			sourceHome, sourceErr := a.require(session.Account)
			if sourceErr != nil {
				return result, sourceErr
			}
			refreshed[session.Account], sourceErr = sessionsInProject(sourceHome, plan.Project)
			if sourceErr != nil {
				return result, sourceErr
			}
		}
		latest, ok := findSessionByID(refreshed[session.Account], session.ID)
		if !ok {
			return result, fmt.Errorf("session %s disappeared from account %s", session.ID, session.Account)
		}
		latest.Account = session.Account
		_, copied, copyErr := a.copySession(session.Account, plan.Target, latest)
		if copyErr != nil {
			return result, copyErr
		}
		if copied {
			changed[session.ID] = true
		}
	}
	activeTargetSessions := map[string]bool{}
	currentManaged, managedErr := a.managedForProject(plan.Project)
	if managedErr != nil {
		return result, managedErr
	}
	for _, record := range currentManaged {
		if record.Account == plan.Target && record.SessionID != "" {
			activeTargetSessions[record.SessionID] = true
		}
	}
	// A resumed managed process registers its own thread metadata. Register the
	// remaining copied conversations through Codex's app-server so they also
	// appear in the destination account's resume picker.
	for _, session := range plan.Sessions {
		if activeTargetSessions[session.ID] {
			continue
		}
		if err = a.indexTransferredSession(plan.Target, session.ID); err != nil {
			return result, fmt.Errorf("register transferred session %s: %w", session.ID, err)
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
