package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectScopesHandleHomeAndFilesystemRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !isHomeScope(home) {
		t.Fatal("did not identify the home directory scope")
	}
	if err = validateProjectPinScope(home); err == nil {
		t.Fatal("accepted the home directory for a project pin")
	}
	if err = validateHandoffScope(home); err != nil {
		t.Fatal("rejected a home session handoff", err)
	}
	volumeRoot := filepath.VolumeName(home) + string(os.PathSeparator)
	if err = validateProjectPinScope(volumeRoot); err == nil {
		t.Fatal("accepted the filesystem root for a project pin")
	}
	if err = validateHandoffScope(volumeRoot); err == nil {
		t.Fatal("accepted the filesystem root for a session handoff")
	}
	if err = validateProjectPinScope(t.TempDir()); err != nil {
		t.Fatal("rejected a regular project directory", err)
	}
}

func TestProjectAccountResolutionAndPrecedence(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	ready(t, a, "other")
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if pinned, err := a.pinProject(nested, "work"); err != nil || pinned != root {
		t.Fatal(pinned, err)
	}
	exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(exclude), projectAccountExclude) {
		t.Fatal("project account was not excluded from local Git tracking", err)
	}
	if got, err := a.accountForDirectory(nested); err != nil || got != "work" {
		t.Fatal(got, err)
	}
	if err := atomicWrite(filepath.Join(root, "services", projectAccountFile), []byte("other\n")); err != nil {
		t.Fatal(err)
	}
	selection, found, err := a.projectSelection(nested)
	if err != nil || !found || selection.Account != "other" || selection.File != filepath.Join(root, "services", projectAccountFile) {
		t.Fatal(selection, found, err)
	}
	if err = a.selectAccount("other"); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if got, err := a.accountForDirectory(outside); err != nil || got != "other" {
		t.Fatal(got, err)
	}
}

func TestLegacyHomePinDoesNotOverrideGlobalSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a := fixture(t)
	ready(t, a, "work")
	if err := atomicWrite(filepath.Join(home, projectAccountFile), []byte("work\n")); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(home, "projects", "example")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if selection, found, err := a.projectSelection(nested); err != nil || found {
		t.Fatal("legacy home pin still shadows the global selection", selection, found, err)
	}
	if got, err := a.accountForDirectory(nested); err != nil || got != "default" {
		t.Fatal("global selection was not used", got, err)
	}
}

func TestProjectAccountExclusionSupportsLinkedWorktreeMetadata(t *testing.T) {
	common := t.TempDir()
	gitDir := filepath.Join(common, "worktrees", "example")
	if err := os.MkdirAll(gitDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+gitDir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureProjectAccountExcluded(root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(common, "info", "exclude"))
	if err != nil || !strings.Contains(string(data), projectAccountExclude) {
		t.Fatal("linked worktree exclusion was not written to the common Git directory", err)
	}
}

func TestProjectAccountRejectsUnsafeAndUnavailablePins(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, projectAccountFile)
	if err := os.WriteFile(path, []byte("missing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.accountForDirectory(root); err == nil {
		t.Fatal("accepted missing project account")
	}
	if err := os.WriteFile(path, []byte("../escape\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.accountForDirectory(root); err == nil {
		t.Fatal("accepted invalid project account")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := a.accountForDirectory(root); err == nil {
		t.Fatal("accepted symlink project account")
	}
	if _, err := a.pinProject(root, "work"); err == nil {
		t.Fatal("replaced symlink project account")
	}
	if _, err := clearProject(root); err == nil {
		t.Fatal("removed symlink project account")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pinProject(root, "work"); err != nil {
		t.Fatal(err)
	}
	if err := a.setEnabled("work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.accountForDirectory(root); err == nil {
		t.Fatal("accepted disabled project account")
	}
}

func TestClearProjectAccount(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pinProject(root, "work"); err != nil {
		t.Fatal(err)
	}
	if cleared, err := clearProject(root); err != nil || cleared != root {
		t.Fatal(cleared, err)
	}
	if exists(filepath.Join(root, projectAccountFile)) {
		t.Fatal("project account file remains")
	}
	if _, err := clearProject(root); err != nil {
		t.Fatal("clear should be idempotent", err)
	}
}

func TestPanelMarksEffectiveProjectAccountActive(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pinProject(project, "work"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	p := Panel{Mode: "home", Records: map[string]Record{}, Width: 100, Height: 30}
	s, err := a.settings()
	if err != nil {
		t.Fatal(err)
	}
	lines, _ := p.accountLines(a, a.names(), s, time.Now())
	view := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(view, "work   ● active") {
		t.Fatalf("project account was not marked active:\n%s", view)
	}
	if !strings.Contains(view, "default   (global default)") {
		t.Fatalf("global selection was not identified separately:\n%s", view)
	}
}

func TestPanelExplainsGlobalSwitchInsidePinnedProject(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	if _, err := a.pinProject(project, "work"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	p := Panel{Mode: "switch", Records: map[string]Record{}, Width: 100, Height: 30}
	names := a.names()
	for index, name := range names {
		if name == "default" {
			p.Cursor = index
		}
	}
	if _, err := p.key(a, names, "enter"); err != nil {
		t.Fatal(err)
	}
	if p.Message != "Global default changed to default; this project remains on work." {
		t.Fatal("switch message did not explain project precedence", p.Message)
	}
}
