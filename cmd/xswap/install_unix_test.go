//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func isolatedInstaller(t *testing.T) *App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TERM", "dumb")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")
	a := fixture(t)
	a.Binary = filepath.Join(t.TempDir(), "xswap")
	if err := os.WriteFile(a.Binary, []byte("fake manager"), 0755); err != nil {
		t.Fatal(err)
	}
	var err error
	a.Binary, err = filepath.EvalSymlinks(a.Binary)
	if err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(cli, []byte("fake original"), 0755); err != nil {
		t.Fatal(err)
	}
	cli, err = filepath.EvalSymlinks(cli)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(a.Root, "installation.json"), map[string]string{"cli": cli, "manager": a.Binary}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestInstallerRepairAndUninstallInTemporaryHome(t *testing.T) {
	a := isolatedInstaller(t)
	ready(t, a, "work")
	home, _ := os.UserHomeDir()
	config := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(config, []byte("export EXISTING=value\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if a.codexIntegrationIssue() == "" {
		t.Fatal("missing integration was reported as healthy")
	}
	if err := a.install(); err != nil {
		t.Fatal(err)
	}
	if err := a.install(); err != nil {
		t.Fatal("repair", err)
	}
	legacyDirectory := filepath.Join(home, ".local", "bin")
	for _, directory := range []string{legacyDirectory, a.durableBin()} {
		for _, name := range []string{"xswap", "codex-swap", "codex"} {
			target, err := filepath.EvalSymlinks(filepath.Join(directory, name))
			if err != nil || target != a.Binary {
				t.Fatal(directory, name, target, err)
			}
		}
	}
	configured, err := os.ReadFile(config)
	if err != nil || !strings.Contains(string(configured), "export EXISTING=value") || strings.Count(string(configured), shellBlockStart) != 1 {
		t.Fatal("shell configuration was not preserved idempotently", err, string(configured))
	}
	info, err := os.Stat(config)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatal("shell configuration mode changed", err)
	}
	if issue := a.codexIntegrationIssue(); issue != "" {
		t.Fatal(issue)
	}
	if err = os.Remove(filepath.Join(a.durableBin(), "codex")); err != nil {
		t.Fatal(err)
	}
	if a.codexIntegrationIssue() == "" {
		t.Fatal("missing durable wrapper was reported as healthy")
	}
	if err = a.install(); err != nil {
		t.Fatal("one-step repair failed", err)
	}
	if issue := a.codexIntegrationIssue(); issue != "" {
		t.Fatal("repair did not restore integration", issue)
	}
	cli, err := a.original()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.uninstall(); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(legacyDirectory, "xswap")) || exists(filepath.Join(legacyDirectory, "codex-swap")) || exists(a.durableBin()) {
		t.Fatal("manager wrappers remain")
	}
	restored, err := filepath.EvalSymlinks(filepath.Join(legacyDirectory, "codex"))
	if err != nil || restored != cli {
		t.Fatal("original not restored", restored, err)
	}
	profile, _ := a.profile("work")
	if !exists(filepath.Join(profile, "auth.json")) {
		t.Fatal("uninstall lost account")
	}
	configured, err = os.ReadFile(config)
	if err != nil || string(configured) != "export EXISTING=value\n" {
		t.Fatal("uninstall did not restore shell configuration", err, string(configured))
	}
}

func TestUnixInstallerSurvivesOfficialCLIUpdate(t *testing.T) {
	a := isolatedInstaller(t)
	if err := a.install(); err != nil {
		t.Fatal(err)
	}
	cli, err := a.original()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cli, []byte("updated official codex"), 0755); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	legacy := filepath.Join(home, ".local", "bin", "codex")
	if err = os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(cli, legacy); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", a.durableBin()+string(os.PathListSeparator)+filepath.Dir(legacy))
	found, err := exec.LookPath("codex")
	if err != nil || !sameExecutablePath(found, a.Binary) {
		t.Fatal("updated CLI displaced the durable wrapper", found, err)
	}
	updated, err := os.ReadFile(cli)
	if err != nil || string(updated) != "updated official codex" {
		t.Fatal("official Codex update was not preserved", err)
	}
	if issue := a.codexIntegrationIssue(); issue != "" {
		t.Fatal("legacy CLI update incorrectly marked integration unhealthy", issue)
	}
}

func TestPanelRepairsMissingCodexIntegration(t *testing.T) {
	a := isolatedInstaller(t)
	var output strings.Builder
	result := a.runPanelAction("repair-install", strings.NewReader("\n"), &output)
	if !strings.Contains(result.Message, "repaired") || !strings.Contains(output.String(), "Repairing") {
		t.Fatal("panel repair did not report success", result, output.String())
	}
	if issue := a.codexIntegrationIssue(); issue != "" {
		t.Fatal("panel repair left integration unhealthy", issue)
	}
}

func TestUnixInstallerRejectsUnsafeShellConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		symlink       bool
	}{
		{name: "incomplete block", content: shellBlockStart + "\n"},
		{name: "symlink", content: "existing\n", symlink: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := isolatedInstaller(t)
			home, _ := os.UserHomeDir()
			config := filepath.Join(home, ".zshrc")
			if tc.symlink {
				target := filepath.Join(t.TempDir(), "zshrc")
				if err := os.WriteFile(target, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, config); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(config, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := a.install(); err == nil {
				t.Fatal("unsafe shell configuration was accepted")
			}
		})
	}
}

func TestUnixInstallerKeepsStableHomebrewLink(t *testing.T) {
	a := isolatedInstaller(t)
	cellarBinary := a.Binary
	stable := filepath.Join(t.TempDir(), "opt", "xswap", "bin", "xswap")
	if err := os.MkdirAll(filepath.Dir(stable), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cellarBinary, stable); err != nil {
		t.Fatal(err)
	}
	a.Binary = stable
	a.PackageManager = "homebrew"
	if err := a.install(); err != nil {
		t.Fatal(err)
	}
	if err := a.install(); err != nil {
		t.Fatal("repair through stable package link", err)
	}
	home, _ := os.UserHomeDir()
	installed := filepath.Join(home, ".local", "bin", "xswap")
	target, err := os.Readlink(installed)
	if err != nil || target != stable {
		t.Fatal("installer did not retain stable Homebrew path", target, err)
	}
	if err := a.uninstall(); err != nil {
		t.Fatal(err)
	}
	if exists(installed) {
		t.Fatal("Homebrew-managed wrapper remained after uninstall")
	}
}

func TestInstallerRefusesUnrelatedExecutables(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular file", true: "foreign symlink"}[symlink], func(t *testing.T) {
			a := isolatedInstaller(t)
			home, _ := os.UserHomeDir()
			directory := filepath.Join(home, ".local", "bin")
			if err := os.MkdirAll(directory, 0755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(directory, "xswap")
			if symlink {
				other := filepath.Join(t.TempDir(), "other")
				if err := os.WriteFile(other, []byte("other"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, target); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(target, []byte("other"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.install(); err == nil {
				t.Fatal("overwrote unrelated executable")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "other" {
				t.Fatal("changed unrelated executable", err)
			}
		})
	}
}

func TestInstallerRequiresOriginalCLI(t *testing.T) {
	a := isolatedInstaller(t)
	if err := os.Remove(filepath.Join(a.Root, "installation.json")); err != nil {
		t.Fatal(err)
	}
	if err := a.install(); err == nil {
		t.Fatal("installed without official CLI")
	}
}
