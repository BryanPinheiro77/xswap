//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func managerPermissionDoctorCheck(path string, info os.FileInfo) (doctorStatus, string, string) {
	if info.Mode().Perm()&0077 != 0 {
		return doctorWarning, "group or other users can access the manager directory", fmt.Sprintf("restrict access with: chmod 700 %q", path)
	}
	if info.Mode().Perm()&0700 != 0700 {
		return doctorWarning, "the current user may not have full access to the manager directory", fmt.Sprintf("restore owner access with: chmod 700 %q", path)
	}
	return doctorPass, "accessible; manager directory mode is owner-only", ""
}

func (a *App) wrapperDoctorCheck() doctorCheck {
	check := doctorCheck{Name: "wrappers", Status: doctorPass}
	var metadata unixInstallation
	if err := readJSON(filepath.Join(a.Root, "installation.json"), &metadata); err != nil {
		check.Status = doctorWarning
		check.Detail = "XSwap installation metadata is unavailable"
		check.Fix = "install or repair the command integration with: xswap install"
		return check
	}
	directory := metadata.Bin
	if directory == "" {
		directory = a.durableBin()
	}
	broken := []string{}
	for _, name := range []string{"codex", "xswap", "codex-swap"} {
		if !linkTargets(filepath.Join(directory, name), a.Binary) {
			broken = append(broken, name)
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		for _, name := range []string{"xswap", "codex-swap"} {
			if !linkTargets(filepath.Join(home, ".local", "bin", name), a.Binary) {
				broken = append(broken, filepath.Join("~", ".local", "bin", name))
			}
		}
		for _, config := range shellConfigs(home) {
			if !shellConfigHasBlock(config, directory) {
				broken = append(broken, "shell PATH integration")
			}
		}
	}
	if len(broken) > 0 {
		check.Status = doctorWarning
		check.Detail = "missing or changed: " + joinDoctorItems(broken)
		check.Fix = "repair the XSwap command links and shell PATH with: xswap install"
		return check
	}
	check.Detail = "Codex and XSwap command links and shell PATH are configured"
	return check
}

func setupDoctorWrappers(t *testing.T, a *App) error {
	t.Helper()
	home := filepath.Dir(a.DefaultHome)
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	directory := a.durableBin()
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	for _, name := range []string{"codex", "xswap", "codex-swap"} {
		if err := os.Symlink(a.Binary, filepath.Join(directory, name)); err != nil {
			return err
		}
	}
	legacy := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		return err
	}
	for _, name := range []string{"xswap", "codex-swap"} {
		if err := os.Symlink(a.Binary, filepath.Join(legacy, name)); err != nil {
			return err
		}
	}
	config := shellConfigs(home)[0]
	return os.WriteFile(config.Path, []byte(shellBlock(directory, config.Fish)+"\n"), 0600)
}

func breakDoctorWrapper(_ *testing.T, a *App) error {
	path := filepath.Join(a.durableBin(), "codex")
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.Symlink(filepath.Join(filepath.Dir(a.Binary), "different-codex"), path)
}

func joinDoctorItems(items []string) string {
	return strings.Join(items, ", ")
}
