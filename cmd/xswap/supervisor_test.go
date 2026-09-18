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
	a.SessionIndexer = func(string, string) error { return nil }
	plan, err := a.planProjectHandoff(project, "work")
	if err != nil || len(plan.Sessions) != 2 || len(plan.Managed) != 0 {
		t.Fatal(plan, err)
	}
	result, err := a.requestProjectHandoff(plan)
	if err != nil || result.Copied != 2 || result.Restarted != 0 {
		t.Fatal(result, err)
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

func TestProjectSwitchPanelExplainsConsequencesBeforeAction(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	t.Chdir(project)
	writeTestSession(t, a.DefaultHome, "2026/09/17", "66666666-6666-4666-8666-666666666666", project, "one")
	p := Panel{Mode: "project-switch", Records: map[string]Record{}, Width: 100, Height: 30, Cursor: 1}
	names := a.names()
	if _, err := p.key(a, names, "enter"); err != nil {
		t.Fatal(err)
	}
	if p.Mode != "confirm-project-switch" || p.Pending != "work" || p.HandoffSessions != 1 {
		t.Fatal(p.Mode, p.Pending, p.HandoffSessions)
	}
	s, _ := a.settings()
	view := stripANSI(p.render(a, names, s, time.Now()))
	for _, phrase := range []string{"Copy 1 project conversation", "Restart and resume 0", "Other projects"} {
		if !strings.Contains(view, phrase) {
			t.Fatalf("confirmation omitted %q:\n%s", phrase, view)
		}
	}
	action, err := p.key(a, names, "y")
	if err != nil || action != "project-handoff:work" {
		t.Fatal(action, err)
	}
}
