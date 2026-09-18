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
	destination, copied, err := a.copySession("default", "work", sessions[0])
	if err != nil || !copied || !strings.Contains(destination, filepath.Join("profiles", "work", "sessions")) {
		t.Fatal(destination, copied, err)
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

func TestIndexTransferredSessionUsesThreadResume(t *testing.T) {
	a := fixture(t)
	helperCLI(t, a)
	id := "abababab-abab-4bab-8bab-abababababab"
	if err := a.indexTransferredSession("default", id); err != nil {
		t.Fatal(err)
	}
}

func appendSessionEvent(t *testing.T, path, message string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fmt.Fprintf(file, "{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":%q}}\n", message)
	closeErr := file.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestSessionTransferFastForwardsRoundTripAndRejectsDivergence(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	id := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	originalPath := writeTestSession(t, a.DefaultHome, "2026/09/17", id, project, "start")
	original, err := readSession(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	workPath, copied, err := a.copySession("default", "work", original)
	if err != nil || !copied {
		t.Fatal(workPath, copied, err)
	}

	appendSessionEvent(t, workPath, "continued in work")
	if _, copied, err = a.copySession("default", "work", original); err != nil || copied {
		t.Fatal("did not preserve destination that was already ahead", copied, err)
	}
	workSession, err := readSession(workPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, copied, err = a.copySession("work", "default", workSession); err != nil || !copied {
		t.Fatal("failed to fast-forward original account", copied, err)
	}
	assertSameFile(t, originalPath, workPath)

	appendSessionEvent(t, originalPath, "continued back in default")
	original, err = readSession(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, copied, err = a.copySession("default", "work", original); err != nil || !copied {
		t.Fatal("failed to fast-forward work account", copied, err)
	}
	assertSameFile(t, originalPath, workPath)

	appendSessionEvent(t, originalPath, "default branch")
	appendSessionEvent(t, workPath, "work branch")
	original, err = readSession(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = a.copySession("default", "work", original); err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatal("accepted divergent session histories", err)
	}
}

func assertSameFile(t *testing.T, left, right string) {
	t.Helper()
	leftData, leftErr := os.ReadFile(left)
	rightData, rightErr := os.ReadFile(right)
	if leftErr != nil || rightErr != nil || string(leftData) != string(rightData) {
		t.Fatal("session files differ", leftErr, rightErr)
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
