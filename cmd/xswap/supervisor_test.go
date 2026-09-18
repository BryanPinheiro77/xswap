package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResumeSessionID(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"resume", id}, id},
		{[]string{"-c", "example=true", "resume", id, "-C", "/tmp/project"}, id},
		{[]string{"resume", "--last"}, ""},
		{[]string{"exec", "resume", "something"}, ""},
	} {
		if got := resumeSessionID(test.args); got != test.want {
			t.Fatalf("resumeSessionID(%v) = %q, want %q", test.args, got, test.want)
		}
	}
}

func TestIdentifyManagedSession(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	cwd := filepath.Join(project, "service")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	id := "22222222-2222-4222-8222-222222222222"
	path := writeTestSession(t, home, "2026/09/17", id, cwd, "managed")
	started := time.Date(2026, 9, 17, 14, 59, 58, 0, time.UTC)
	session, err := identifyManagedSession(home, project, cwd, "", started)
	if err != nil || session.ID != id {
		t.Fatal(session, err)
	}
	if session, err = identifyManagedSession(home, project, cwd, id, time.Time{}); err != nil || session.Path != path {
		t.Fatal(session, err)
	}
	writeTestSession(t, home, "2026/09/18", "33333333-3333-4333-8333-333333333333", cwd, "ambiguous")
	if _, err = identifyManagedSession(home, project, cwd, "", started); err == nil || !strings.Contains(err.Error(), "found 2 candidates") {
		t.Fatal("ambiguous session was accepted", err)
	}
}

func TestProjectHandoffCopiesAllProjectSessionsAndPinsTarget(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	writeTestSession(t, a.DefaultHome, "2026/09/17", "44444444-4444-4444-8444-444444444444", project, "one")
	writeTestSession(t, a.DefaultHome, "2026/09/18", "55555555-5555-4555-8555-555555555555", filepath.Join(project, "nested"), "two")
	indexed := map[string]bool{}
	a.SessionIndexer = func(account, id string) error {
		if account != "work" {
			t.Fatalf("indexed account %q, want work", account)
		}
		indexed[id] = true
		return nil
	}
	plan, err := a.planProjectHandoff(project, "work")
	if err != nil || len(plan.Sessions) != 2 || len(plan.Managed) != 0 {
		t.Fatal(plan, err)
	}
	result, err := a.requestProjectHandoff(plan)
	if err != nil || result.Copied != 2 || result.Restarted != 0 {
		t.Fatal(result, err)
	}
	if len(indexed) != 2 {
		t.Fatal("did not register every transferred session", indexed)
	}
	if selected, err := a.accountForDirectory(project); err != nil || selected != "work" {
		t.Fatal(selected, err)
	}
	home, _ := a.profile("work")
	sessions, err := sessionsInProject(home, project)
	if err != nil || len(sessions) != 2 {
		t.Fatal(sessions, err)
	}
}

func TestProjectHandoffCombinesSessionsFromMultipleAccounts(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	ready(t, a, "other")
	project := t.TempDir()
	defaultID := "18181818-1818-4818-8818-181818181818"
	workID := "19191919-1919-4919-8919-191919191919"
	writeTestSession(t, a.DefaultHome, "2026/09/17", defaultID, project, "from default")
	workHome, _ := a.profile("work")
	writeTestSession(t, workHome, "2026/09/18", workID, project, "from work")
	indexed := map[string]bool{}
	a.SessionIndexer = func(account, id string) error {
		indexed[account+":"+id] = true
		return nil
	}
	plan, err := a.planProjectHandoff(project, "other")
	if err != nil || len(plan.Sessions) != 2 || strings.Join(plan.Sources, ",") != "default,work" {
		t.Fatal("multi-account sessions were not combined", plan, err)
	}
	result, err := a.requestProjectHandoff(plan)
	if err != nil || result.Copied != 2 || result.Sessions != 2 {
		t.Fatal(result, err)
	}
	otherHome, _ := a.profile("other")
	sessions, err := sessionsInProject(otherHome, project)
	if err != nil || len(sessions) != 2 {
		t.Fatal("destination did not receive every source account session", sessions, err)
	}
	if !indexed["other:"+defaultID] || !indexed["other:"+workID] {
		t.Fatal("transferred sessions were not indexed", indexed)
	}
}

func TestProjectHandoffKeepsSessionsAlreadyInDestination(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	a.SessionIndexer = func(string, string) error { return nil }
	project := t.TempDir()
	writeTestSession(t, a.DefaultHome, "2026/09/17", "20202020-2020-4020-8020-202020202020", project, "already in default")
	workHome, _ := a.profile("work")
	writeTestSession(t, workHome, "2026/09/18", "21212121-2121-4121-8121-212121212121", project, "move from work")
	plan, err := a.planProjectHandoff(project, "default")
	if err != nil || len(plan.Sessions) != 2 {
		t.Fatal(plan, err)
	}
	result, err := a.requestProjectHandoff(plan)
	if err != nil || result.Copied != 1 || result.Already != 1 {
		t.Fatal(result, err)
	}
	sessions, err := sessionsInProject(a.DefaultHome, project)
	if err != nil || len(sessions) != 2 {
		t.Fatal("destination did not retain and combine sessions", sessions, err)
	}
}

func TestProjectHandoffFindsProjectSessionsOutsideSelectedAccount(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	if err := a.selectAccount("work"); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	id := "12121212-1212-4212-8212-121212121212"
	writeTestSession(t, a.DefaultHome, "2026/09/18", id, project, "project conversation")
	plan, err := a.projectHandoffSource(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Sources) != 1 || plan.Sources[0] != "default" || len(plan.Sessions) != 1 || plan.Sessions[0].ID != id {
		t.Fatal("did not find the project's conversation in its actual account", plan)
	}
}

func TestSessionProjectsComeOnlyFromExistingCodexSessions(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	first := t.TempDir()
	second := t.TempDir()
	unused := t.TempDir()
	for _, project := range []string{first, second} {
		if err := os.Mkdir(filepath.Join(project, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(first, "service"), 0700); err != nil {
		t.Fatal(err)
	}
	firstID := "13131313-1313-4313-8313-131313131313"
	writeTestSession(t, a.DefaultHome, "2026/09/17", firstID, filepath.Join(first, "service"), "first project")
	writeTestSession(t, a.DefaultHome, "2026/09/18", "17171717-1717-4717-8717-171717171717", first, "default-only conversation")
	workHome, _ := a.profile("work")
	writeTestSession(t, workHome, "2026/09/17", firstID, filepath.Join(first, "service"), "copied first project")
	writeTestSession(t, a.DefaultHome, "2026/09/18", "14141414-1414-4414-8414-141414141414", second, "second project")
	missing := filepath.Join(t.TempDir(), "removed")
	writeTestSession(t, a.DefaultHome, "2026/09/18", "15151515-1515-4515-8515-151515151515", missing, "removed project")
	if err := a.selectAccount("work"); err != nil {
		t.Fatal(err)
	}

	projects, err := a.sessionProjects()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	accounts := map[string]int{}
	for _, project := range projects {
		counts[project.Root] = project.Count
		accounts[project.Root] = project.Accounts
	}
	if counts[first] != 2 || counts[second] != 1 {
		t.Fatal("project counts did not include unique sessions from every account", counts)
	}
	if accounts[first] != 2 || accounts[second] != 1 {
		t.Fatal("project account counts were incorrect", accounts)
	}
	if _, ok := counts[unused]; ok {
		t.Fatal("directory without Codex sessions was listed")
	}
	if _, ok := counts[missing]; ok {
		t.Fatal("removed project was listed")
	}
}

func TestProjectHandoffBlocksOpenSessionOutsideSupervisor(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	id := "34343434-3434-4434-8434-343434343434"
	writeTestSession(t, a.DefaultHome, "2026/09/18", id, project, "legacy open conversation")
	a.SessionActive = func(home, sessionID string) (bool, error) {
		return home == a.DefaultHome && sessionID == id, nil
	}
	plan, err := a.planProjectHandoff(project, "work")
	if err != nil || len(plan.Unmanaged) != 1 || plan.Unmanaged[0].ID != id {
		t.Fatal("open unmanaged conversation was not identified", plan, err)
	}
	if _, err = a.requestProjectHandoff(plan); err == nil || !strings.Contains(err.Error(), "legacy open conversation") {
		t.Fatal("handoff did not name and block the open unmanaged conversation", err)
	}
	if exists(filepath.Join(project, projectAccountFile)) {
		t.Fatal("blocked handoff changed the project account")
	}
}

func TestPanelExplainsOpenSessionOutsideSupervisor(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	t.Chdir(project)
	id := "45454545-4545-4454-8454-454545454545"
	writeTestSession(t, a.DefaultHome, "2026/09/18", id, project, "restart this conversation")
	a.SessionActive = func(string, string) (bool, error) { return true, nil }
	p := Panel{Mode: "home", Records: map[string]Record{}, Width: 100, Height: 30}
	names := a.names()
	for index, item := range p.menu() {
		if item == "Continue sessions with another account…" {
			p.MenuCursor = index
		}
	}
	if _, err := p.key(a, names, "enter"); err != nil || p.Mode != "session-select" || !p.HandoffUnmanaged[id] {
		t.Fatal("session picker did not mark the unmanaged conversation", p.Mode, err)
	}
	if _, err := p.key(a, names, "enter"); err != nil || p.Mode != "unmanaged-warning" {
		t.Fatal("panel did not open the restart warning", p.Mode, err)
	}
	s, _ := a.settings()
	view := stripANSI(p.render(a, names, s, time.Now()))
	for _, phrase := range []string{"restart this conversation", "codex resume", "No conversation was copied"} {
		if !strings.Contains(view, phrase) {
			t.Fatalf("warning omitted %q:\n%s", phrase, view)
		}
	}
}

func TestProjectHandoffRejectsDivergenceBeforeChangingProjectAccount(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	a.SessionIndexer = func(string, string) error { return nil }
	project := t.TempDir()
	id := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	sourcePath := writeTestSession(t, a.DefaultHome, "2026/09/18", id, project, "start")
	session, err := readSession(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	destination, _, err := a.copySession("default", "work", session)
	if err != nil {
		t.Fatal(err)
	}
	appendSessionEvent(t, sourcePath, "continued in default")
	appendSessionEvent(t, destination, "continued in work")
	if _, err = a.planProjectHandoff(project, "work"); err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatal("handoff accepted divergent histories", err)
	}
	if selected, selectionErr := a.accountForDirectory(project); selectionErr != nil || selected != "default" {
		t.Fatal("failed handoff changed project account", selected, selectionErr)
	}
	if exists(filepath.Join(project, projectAccountFile)) {
		t.Fatal("failed handoff created a project account pin")
	}
}

func TestHomeSessionHandoffUsesGlobalSelectionWithoutProjectPin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a := fixture(t)
	ready(t, a, "work")
	indexed := ""
	a.SessionIndexer = func(account, id string) error {
		indexed = account + ":" + id
		return nil
	}
	writeTestSession(t, a.DefaultHome, "2026/09/18", "ffffffff-ffff-4fff-8fff-ffffffffffff", home, "standalone conversation")
	plan, err := a.planProjectHandoff(home, "work")
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.requestProjectHandoff(plan)
	if err != nil || !result.Global || result.Copied != 1 {
		t.Fatal(result, err)
	}
	if indexed != "work:ffffffff-ffff-4fff-8fff-ffffffffffff" {
		t.Fatal("standalone session was not registered", indexed)
	}
	if a.selected() != "work" {
		t.Fatal("home handoff did not update the global account")
	}
	if exists(filepath.Join(home, projectAccountFile)) {
		t.Fatal("home handoff created a broad project pin")
	}
}

func TestProjectSwitchPanelExplainsConsequencesBeforeAction(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	t.Chdir(project)
	writeTestSession(t, a.DefaultHome, "2026/09/17", "66666666-6666-4666-8666-666666666666", project, "one")
	p := Panel{Mode: "home", Records: map[string]Record{}, Width: 100, Height: 30}
	names := a.names()
	for index, item := range p.menu() {
		if item == "Continue sessions with another account…" {
			p.MenuCursor = index
		}
	}
	if _, err := p.key(a, names, "enter"); err != nil {
		t.Fatal(err)
	}
	if p.Mode != "session-select" || len(p.HandoffSessions) != 1 || len(p.selectedSessions()) != 1 {
		t.Fatal(p.Mode, len(p.HandoffSessions), len(p.selectedSessions()))
	}
	p.key(a, names, "a")
	if len(p.selectedSessions()) != 0 {
		t.Fatal("select-all shortcut did not clear the selection")
	}
	p.key(a, names, "a")
	if len(p.selectedSessions()) != 1 {
		t.Fatal("select-all shortcut did not restore the selection")
	}
	s, _ := a.settings()
	selection := stripANSI(p.render(a, names, s, time.Now()))
	if !strings.Contains(selection, "[✓]") || !strings.Contains(selection, "one") || !strings.Contains(selection, "from default") {
		t.Fatalf("session selection omitted the selected conversation:\n%s", selection)
	}
	p.key(a, names, " ")
	if len(p.selectedSessions()) != 0 {
		t.Fatal("space did not unselect the conversation")
	}
	p.key(a, names, "enter")
	if p.Mode != "session-select" || !strings.Contains(p.Message, "Select at least one") {
		t.Fatal("continued without a selected conversation", p.Mode, p.Message)
	}
	p.key(a, names, " ")
	p.key(a, names, "enter")
	if p.Mode != "project-switch" || names[p.Cursor] != "work" {
		t.Fatal("destination account step did not open", p.Mode, p.Cursor)
	}
	if _, err := p.key(a, names, "enter"); err != nil {
		t.Fatal(err)
	}
	if p.Mode != "confirm-project-switch" || p.Pending != "work" || len(p.selectedSessions()) != 1 {
		t.Fatal(p.Mode, p.Pending, len(p.selectedSessions()))
	}
	view := stripANSI(p.render(a, names, s, time.Now()))
	for _, phrase := range []string{"Selected conversations: 1", "one", "Restart and resume 0", "Other projects"} {
		if !strings.Contains(view, phrase) {
			t.Fatalf("confirmation omitted %q:\n%s", phrase, view)
		}
	}
	action, err := p.key(a, names, "enter")
	actionProject, target, selected, parseErr := parseHandoffAction(action)
	if err != nil || parseErr != nil || actionProject != project || target != "work" || !selected["66666666-6666-4666-8666-666666666666"] {
		t.Fatal(action, err)
	}
}

func TestHomePanelSelectsProjectBeforeSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	writeTestSession(t, a.DefaultHome, "2026/09/18", "16161616-1616-4616-8616-161616161616", project, "select this project")
	t.Chdir(home)
	p := Panel{Mode: "home", Records: map[string]Record{}, Width: 100, Height: 30}
	names := a.names()
	for index, item := range p.menu() {
		if item == "Continue sessions with another account…" {
			p.MenuCursor = index
		}
	}
	if _, err := p.key(a, names, "enter"); err != nil || p.Mode != "project-select" || len(p.HandoffProjects) != 1 {
		t.Fatal("home panel did not open the project picker", p.Mode, len(p.HandoffProjects), err)
	}
	if _, err := p.key(a, names, "enter"); err != nil || p.Mode != "session-select" || p.HandoffProject != project {
		t.Fatal("project picker did not open its conversations", p.Mode, p.HandoffProject, err)
	}
}

func TestPanelShowsProjectSessionsFromEveryAccount(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	writeTestSession(t, a.DefaultHome, "2026/09/17", "22222222-3333-4333-8333-222222222222", project, "personal conversation")
	workHome, _ := a.profile("work")
	writeTestSession(t, workHome, "2026/09/18", "23232323-2323-4323-8323-232323232323", project, "work conversation")
	t.Chdir(project)
	p := Panel{Mode: "home", Records: map[string]Record{}, Width: 100, Height: 30}
	names := a.names()
	for index, item := range p.menu() {
		if item == "Continue sessions with another account…" {
			p.MenuCursor = index
		}
	}
	if _, err := p.key(a, names, "enter"); err != nil || p.Mode != "session-select" || len(p.HandoffSessions) != 2 {
		t.Fatal("panel did not combine account sessions", p.Mode, len(p.HandoffSessions), err)
	}
	s, _ := a.settings()
	view := stripANSI(p.render(a, names, s, time.Now()))
	for _, phrase := range []string{"personal conversation", "work conversation", "from default", "from work"} {
		if !strings.Contains(view, phrase) {
			t.Fatalf("multi-account picker omitted %q:\n%s", phrase, view)
		}
	}
}

func TestSelectedProjectHandoffCopiesOnlySelectedSessions(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	firstID := "99999999-9999-4999-8999-999999999999"
	secondID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	writeTestSession(t, a.DefaultHome, "2026/09/17", firstID, project, "selected")
	writeTestSession(t, a.DefaultHome, "2026/09/18", secondID, project, "not selected")
	indexed := []string{}
	a.SessionIndexer = func(account, id string) error {
		indexed = append(indexed, account+":"+id)
		return nil
	}
	plan, err := a.planSelectedProjectHandoff(project, "work", map[string]bool{firstID: true})
	if err != nil || len(plan.Sessions) != 1 || plan.Sessions[0].ID != firstID {
		t.Fatal(plan, err)
	}
	result, err := a.requestProjectHandoff(plan)
	if err != nil || result.Copied != 1 || result.Sessions != 1 {
		t.Fatal(result, err)
	}
	if len(indexed) != 1 || indexed[0] != "work:"+firstID {
		t.Fatal("registered the wrong selection", indexed)
	}
	home, _ := a.profile("work")
	sessions, err := sessionsInProject(home, project)
	if err != nil || len(sessions) != 1 || sessions[0].ID != firstID {
		t.Fatal(sessions, err)
	}
}

func TestFilterHandoffPlanKeepsOnlyMatchingManagedSessions(t *testing.T) {
	first := codexSession{ID: "11111111-1111-4111-8111-111111111111"}
	second := codexSession{ID: "22222222-2222-4222-8222-222222222222"}
	plan := projectHandoffPlan{
		Sessions: []codexSession{first, second},
		Managed:  []managedCodex{{SessionID: first.ID}, {SessionID: second.ID}, {SessionID: ""}},
	}
	filtered, err := filterHandoffPlan(plan, map[string]bool{second.ID: true})
	if err != nil || len(filtered.Sessions) != 1 || filtered.Sessions[0].ID != second.ID || len(filtered.Managed) != 1 || filtered.Managed[0].SessionID != second.ID {
		t.Fatal(filtered, err)
	}
	if _, err = filterHandoffPlan(plan, map[string]bool{}); err == nil {
		t.Fatal("accepted a handoff without selected conversations")
	}
}

func TestFilterHandoffPlanAssociatesOneUnknownProcessWithOneSelectedSession(t *testing.T) {
	project := t.TempDir()
	session := codexSession{ID: "abababab-abab-4bab-8bab-abababababab", CWD: project, Path: "session.jsonl"}
	plan := projectHandoffPlan{
		Sessions: []codexSession{session},
		Managed:  []managedCodex{{CWD: project}},
	}
	filtered, err := filterHandoffPlan(plan, map[string]bool{session.ID: true})
	if err != nil || len(filtered.Managed) != 1 || filtered.Managed[0].SessionID != session.ID {
		t.Fatal("failed to associate selected session with its only managed process", filtered, err)
	}
	plan.Managed = append(plan.Managed, managedCodex{CWD: project})
	filtered, err = filterHandoffPlan(plan, map[string]bool{session.ID: true})
	if err != nil || len(filtered.Managed) != 0 {
		t.Fatal("associated an ambiguous managed process", filtered, err)
	}
}
