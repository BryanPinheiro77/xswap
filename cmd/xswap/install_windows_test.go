//go:build windows

package main

import (
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
	commandLine := `call "` + wrapper + `" exec "hello world"`
	cmd := exec.Command("cmd.exe", "/d", "/c", commandLine)
	cmd.Env = envWith(envWith(os.Environ(), "CODEX_SWAP_HOME", a.Root), "XSWAP_TEST_CAPTURE", capture)
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
