//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func managerPermissionDoctorCheck(_ string, _ os.FileInfo) (doctorStatus, string, string) {
	return doctorPass, "accessible; current-user directory access verified", ""
}

func (a *App) wrapperDoctorCheck() doctorCheck {
	check := doctorCheck{Name: "wrappers", Status: doctorPass}
	directory, err := windowsInstallDir()
	if err != nil {
		check.Status = doctorWarning
		check.Detail = "could not locate the XSwap command directory"
		check.Fix = "repair the command integration with: xswap install"
		return check
	}
	var metadata struct {
		Manager string `json:"manager"`
	}
	if err = readJSON(filepath.Join(a.Root, "installation.json"), &metadata); err != nil {
		check.Status = doctorWarning
		check.Detail = "XSwap installation metadata is unavailable"
		check.Fix = "install or repair the command integration with: xswap install"
		return check
	}
	for _, item := range windowsCommands {
		path := filepath.Join(directory, item.name)
		if !exists(path) || !ownedWindowsWrapper(path, a.Binary, metadata.Manager, item.codex) {
			check.Status = doctorWarning
			check.Detail = "a required XSwap or Codex command wrapper is missing or changed"
			check.Fix = "repair the command integration with: xswap install"
			return check
		}
	}
	check.Detail = "Codex and XSwap command wrappers are configured"
	return check
}

func setupDoctorWrappers(t *testing.T, a *App) error {
	t.Helper()
	local := filepath.Join(filepath.Dir(a.DefaultHome), "local-app-data")
	if err := os.MkdirAll(local, 0700); err != nil {
		return err
	}
	t.Setenv("LOCALAPPDATA", local)
	directory, err := windowsInstallDir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	for _, item := range windowsCommands {
		if err = os.WriteFile(filepath.Join(directory, item.name), windowsWrapper(a.Binary, item.codex), 0600); err != nil {
			return err
		}
	}
	return nil
}

func breakDoctorWrapper(_ *testing.T, a *App) error {
	directory, err := windowsInstallDir()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "codex.cmd"), []byte("@echo off\r\necho replaced\r\n"), 0600)
}
