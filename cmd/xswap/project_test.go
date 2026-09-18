package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
