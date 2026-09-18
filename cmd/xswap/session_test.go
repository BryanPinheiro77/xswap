package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestSession(t *testing.T, home, date, id, cwd, message string) string {
	t.Helper()
	path := filepath.Join(home, "sessions", date, "rollout-2026-09-17T12-00-00-"+id+".jsonl")
	content := fmt.Sprintf("{\"timestamp\":\"2026-09-17T15:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"cwd\":%q,\"timestamp\":\"2026-09-17T15:00:00Z\"}}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":%q}}\n", id, cwd, message)
	if err := atomicWrite(path, []byte(content)); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSessionSkipsTechnicalContextForPreview(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	path := filepath.Join(home, "sessions", "2026/09/17", "rollout-"+id+".jsonl")
	content := fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"cwd\":%q}}\n{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"<environment_context>technical metadata</environment_context>\"}]}}\n{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"# AGENTS.md instructions for /project\"}]}}\n{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"Implement account handoff\"}]}}\n", id, project)
	if err := atomicWrite(path, []byte(content)); err != nil {
		t.Fatal(err)
	}
	session, err := readSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if session.Preview != "Implement account handoff" {
		t.Fatalf("unexpected preview %q", session.Preview)
	}
}

func TestSessionsInProjectAndTransfer(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	nested := filepath.Join(project, "service")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	firstID := "11111111-1111-4111-8111-111111111111"
	secondID := "22222222-2222-4222-8222-222222222222"
	first := writeTestSession(t, a.DefaultHome, "2026/09/17", firstID, project, "first")
	writeTestSession(t, a.DefaultHome, "2026/09/18", secondID, nested, "second")
	writeTestSession(t, a.DefaultHome, "2026/09/19", "33333333-3333-4333-8333-333333333333", t.TempDir(), "other")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(first, old, old); err != nil {
		t.Fatal(err)
	}
	sessions, err := sessionsInProject(a.DefaultHome, project)
	if err != nil || len(sessions) != 2 || sessions[0].ID != secondID || sessions[1].ID != firstID {
		t.Fatal(sessions, err)
	}
	indexed := []string{}
	a.SessionIndexer = func(account, id string) error {
		indexed = append(indexed, account+":"+id)
		return nil
	}
	destination, copied, err := a.copySession("default", "work", sessions[0])
	if err != nil || !copied || !strings.Contains(destination, filepath.Join("profiles", "work", "sessions")) {
		t.Fatal(destination, copied, err)
	}
	if len(indexed) != 1 || indexed[0] != "work:"+secondID {
		t.Fatal(indexed)
	}
	if _, copied, err = a.copySession("default", "work", sessions[0]); err != nil || copied {
		t.Fatal("identical transfer was not idempotent", copied, err)
	}
	if err = os.WriteFile(destination, []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = a.copySession("default", "work", sessions[0]); err == nil {
		t.Fatal("overwrote conflicting destination")
	}
}

func TestSessionTransferRejectsUnsafeFilesAndCleansFailure(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	id := "44444444-4444-4444-8444-444444444444"
	path := writeTestSession(t, a.DefaultHome, "2026/09/17", id, project, "safe")
	session, err := readSession(path)
	if err != nil {
		t.Fatal(err)
	}
	a.SessionIndexer = func(string, string) error { return fmt.Errorf("index failed") }
	destination, _, err := a.copySession("default", "work", session)
	if err == nil || destination != "" {
		t.Fatal("ignored indexing failure", destination, err)
	}
	work, _ := a.profile("work")
	if exists(filepath.Join(work, "sessions", "2026/09/17", filepath.Base(path))) {
		t.Fatal("partial destination remains")
	}
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err = os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	session.Path = outside
	if _, _, err = a.copySession("default", "work", session); err == nil {
		t.Fatal("copied session outside source profile")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err = sessionsInProject(a.DefaultHome, project); err == nil {
		t.Fatal("accepted symlink session")
	}
}

func TestSessionTransferRejectsSymlinkDestinationParent(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	id := "88888888-8888-4888-8888-888888888888"
	path := writeTestSession(t, a.DefaultHome, "2026/09/17", id, project, "safe")
	session, err := readSession(path)
	if err != nil {
		t.Fatal(err)
	}
	work, _ := a.profile("work")
	outside := t.TempDir()
	if err = os.Symlink(outside, filepath.Join(work, "sessions")); err != nil {
		if os.IsPermission(err) {
			t.Skip("symlink creation is unavailable")
		}
		t.Fatal(err)
	}
	if _, _, err = a.copySession("default", "work", session); err == nil {
		t.Fatal("copied a session through a symlink destination")
	}
	if entries, readErr := os.ReadDir(outside); readErr != nil || len(entries) != 0 {
		t.Fatal("wrote outside the destination profile", entries, readErr)
	}
}
