//go:build windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsInstallRepairAndUninstall(t *testing.T) {
	a := fixture(t)
	local := t.TempDir()
	commands := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("PATH", commands)
	t.Setenv("PATHEXT", ".EXE;.CMD")
	cli := filepath.Join(commands, "codex.exe")
	if err := os.WriteFile(cli, []byte("fake codex"), 0755); err != nil {
		t.Fatal(err)
	}
	a.Binary = filepath.Join(t.TempDir(), "xswap.exe")
	if err := os.WriteFile(a.Binary, []byte("fake xswap"), 0755); err != nil {
		t.Fatal(err)
	}

	originalPathSetup := configureWindowsUserPath
	originalPathRemoval := removeWindowsUserPath
	t.Cleanup(func() {
		configureWindowsUserPath = originalPathSetup
		removeWindowsUserPath = originalPathRemoval
	})
	var configured string
	var removed string
	configureWindowsUserPath = func(directory string) error { configured = directory; return nil }
	removeWindowsUserPath = func(directory string) error { removed = directory; return nil }

	if err := a.install(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(local, "XSwap", "bin")
	if configured != directory {
		t.Fatalf("PATH configured for %q, want %q", configured, directory)
	}
	if issue := a.codexIntegrationIssue(); issue != "" {
		t.Fatal(issue)
	}
	if err := os.Remove(filepath.Join(directory, "codex.cmd")); err != nil {
		t.Fatal(err)
	}
	if a.codexIntegrationIssue() == "" {
		t.Fatal("missing Codex wrapper was reported as healthy")
	}
	if err := a.install(); err != nil {
		t.Fatal("one-step repair failed", err)
	}
	if issue := a.codexIntegrationIssue(); issue != "" {
		t.Fatal("repair did not restore integration", issue)
	}
	for _, item := range []struct {
		name  string
		codex bool
	}{{"xswap.cmd", false}, {"codex-swap.cmd", false}, {"codex.cmd", true}} {
		data, err := os.ReadFile(filepath.Join(directory, item.name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(windowsWrapper(a.Binary, item.codex)) {
			t.Fatalf("invalid %s wrapper: %q", item.name, data)
		}
	}
	if err := a.install(); err != nil {
		t.Fatal("repair failed:", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "xswap.cmd"), []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "codex.cmd"), []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if a.codexIntegrationIssue() == "" {
		t.Fatal("foreign Codex wrapper was reported as healthy")
	}
	if err := os.WriteFile(filepath.Join(directory, "codex.cmd"), windowsWrapper(a.Binary, true), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.install(); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatal("foreign wrapper was replaced", err)
	}
	if err := a.uninstall(); err == nil || !strings.Contains(err.Error(), "refusing to remove") {
		t.Fatal("foreign wrapper was removed", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "xswap.cmd"), windowsWrapper(a.Binary, false), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.uninstall(); err != nil {
		t.Fatal(err)
	}
	if removed != directory {
		t.Fatalf("PATH removal for %q, want %q", removed, directory)
	}
	for _, name := range []string{"xswap.cmd", "codex-swap.cmd", "codex.cmd"} {
		if exists(filepath.Join(directory, name)) {
			t.Fatal("wrapper remains", name)
		}
	}
}

func TestWindowsUpdateActivatesVersionedExecutable(t *testing.T) {
	a := fixture(t)
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	directory := filepath.Join(local, "XSwap", "bin")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	a.Binary = filepath.Join(t.TempDir(), "xswap.exe")
	if err := os.WriteFile(a.Binary, []byte("old binary"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range windowsCommands {
		if err := os.WriteFile(filepath.Join(directory, item.name), windowsWrapper(a.Binary, item.codex), 0600); err != nil {
			t.Fatal(err)
		}
	}
	metadata := windowsInstallation{CLI: filepath.Join(t.TempDir(), "codex.exe"), Manager: a.Binary, Bin: directory}
	if err := writeJSON(filepath.Join(a.Root, "installation.json"), metadata); err != nil {
		t.Fatal(err)
	}
	newBinary := []byte("new binary")
	if err := a.replaceInstalledBinary(newBinary, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(local, "XSwap", "app", "xswap-v1.2.3.exe")
	if a.Binary != want {
		t.Fatalf("active binary %q, want %q", a.Binary, want)
	}
	installed, err := os.ReadFile(want)
	if err != nil || string(installed) != string(newBinary) {
		t.Fatal("new executable was not installed", err)
	}
	backup, err := os.ReadFile(filepath.Join(a.Root, "previous-xswap"))
	if err != nil || string(backup) != "old binary" {
		t.Fatal("previous executable was not preserved", err)
	}
	for _, item := range windowsCommands {
		wrapper, readErr := os.ReadFile(filepath.Join(directory, item.name))
		if readErr != nil || string(wrapper) != string(windowsWrapper(want, item.codex)) {
			t.Fatalf("wrapper %s was not activated: %v", item.name, readErr)
		}
	}
	var updated windowsInstallation
	if err = readJSON(filepath.Join(a.Root, "installation.json"), &updated); err != nil || updated.Manager != want {
		t.Fatal("installation metadata was not updated", err)
	}
}

func TestWindowsBatchCommandPreservesArguments(t *testing.T) {
	directory := t.TempDir()
	fakeCodex := filepath.Join(directory, "fake-codex.exe")
	build := exec.Command("go", "build", "-o", fakeCodex, filepath.Join("testdata", "fake_codex.go"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Codex: %v\n%s", err, output)
	}
	batch := filepath.Join(directory, "codex.cmd")
	if err := os.WriteFile(batch, []byte("@echo off\r\n\"%XSWAP_FAKE_CODEX%\" %*\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(directory, "capture.json")
	want := []string{"space value", "rock&roll", "%PATH%", "bang!value", `say "hello"`}
	cmd := processCommand(batch, want...)
	setProcessEnvironment(cmd, envWith(envWith(os.Environ(), "XSWAP_FAKE_CODEX", fakeCodex), "XSWAP_TEST_CAPTURE", capture))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run batch Codex: %v\n%s", err, output)
	}
	var result struct {
		Args []string `json:"args"`
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args %q, want %q", result.Args, want)
	}
}

func TestWindowsLoginAndQuotaQueryThroughBatchCodex(t *testing.T) {
	a := fixture(t)
	directory := t.TempDir()
	fakeCodex := filepath.Join(directory, "fake-codex.exe")
	build := exec.Command("go", "build", "-o", fakeCodex, filepath.Join("testdata", "fake_codex.go"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Codex: %v\n%s", err, output)
	}
	batch := filepath.Join(directory, "codex.cmd")
	if err := os.WriteFile(batch, []byte("@echo off\r\n\"%XSWAP_FAKE_CODEX%\" %*\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XSWAP_FAKE_CODEX", fakeCodex)
	if err := writeJSON(filepath.Join(a.Root, "installation.json"), map[string]string{"cli": batch}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.create("work"); err != nil {
		t.Fatal(err)
	}
	if err := a.login("work", false); err != nil {
		t.Fatal("login through batch Codex:", err)
	}
	record, err := a.readLimits(context.Background(), "work")
	if err != nil {
		t.Fatal("quota query through batch Codex:", err)
	}
	if record.Account.Email != "windows@example.com" || record.Limits.Main == nil || record.Limits.Main.Primary == nil || record.Limits.Main.Primary.Used == nil || *record.Limits.Main.Primary.Used != 12 {
		t.Fatalf("unexpected quota record: %+v", record)
	}
}

func TestWindowsCodexWrapperUsesSelectedAccount(t *testing.T) {
	manager := os.Getenv("XSWAP_WINDOWS_BINARY")
	if manager == "" {
		t.Skip("XSWAP_WINDOWS_BINARY is required for the wrapper integration test")
	}
	a := fixture(t)
	local := t.TempDir()
	commands := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PATHEXT", ".EXE;.CMD")
	a.Binary = manager

	fakeCodex := filepath.Join(commands, "codex.exe")
	build := exec.Command("go", "build", "-o", fakeCodex, filepath.Join("testdata", "fake_codex.go"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Codex: %v\n%s", err, output)
	}
	originalPathSetup := configureWindowsUserPath
	t.Cleanup(func() { configureWindowsUserPath = originalPathSetup })
	configureWindowsUserPath = func(string) error { return nil }
	if err := a.install(); err != nil {
		t.Fatal(err)
	}
	ready(t, a, "work")
	if err := a.selectAccount("work"); err != nil {
		t.Fatal(err)
	}

	capture := filepath.Join(t.TempDir(), "capture.json")
	wrapper := filepath.Join(local, "XSwap", "bin", "codex.cmd")
	cmd := processCommand(wrapper, "exec", "hello world")
	setProcessEnvironment(cmd, envWith(envWith(os.Environ(), "CODEX_SWAP_HOME", a.Root), "XSWAP_TEST_CAPTURE", capture))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run codex wrapper: %v\n%s", err, output)
	}
	var result struct {
		Args      []string `json:"args"`
		CodexHome string   `json:"codexHome"`
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	profile, _ := a.profile("work")
	if result.CodexHome != profile {
		t.Fatalf("CODEX_HOME %q, want %q", result.CodexHome, profile)
	}
	want := []string{"-c", `cli_auth_credentials_store="file"`, "exec", "hello world"}
	if strings.Join(result.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args %q, want %q", result.Args, want)
	}
}
