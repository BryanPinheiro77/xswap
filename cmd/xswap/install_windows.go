//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func windowsInstallDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "XSwap", "bin"), nil
}

func windowsWrapper(manager string, codex bool) []byte {
	command := fmt.Sprintf("\"%s\"", manager)
	if codex {
		command += " __codex"
	}
	return []byte("@echo off\r\n" + command + " %*\r\n")
}

func ownedWindowsWrapper(path, manager, previous string, codex bool) bool {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	if bytes.Equal(data, windowsWrapper(manager, codex)) {
		return true
	}
	return previous != "" && bytes.Equal(data, windowsWrapper(previous, codex))
}

func (a *App) install() error {
	var previous struct {
		Manager string `json:"manager"`
	}
	_ = readJSON(filepath.Join(a.Root, "installation.json"), &previous)
	cli, err := a.original()
	if err != nil {
		cli, err = exec.LookPath("codex")
		if err != nil {
			return errors.New("install the official Codex CLI first")
		}
		if strings.EqualFold(cli, a.Binary) {
			return errors.New("cannot locate the original Codex CLI")
		}
	}
	directory, err := windowsInstallDir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	for _, item := range []struct {
		name  string
		codex bool
	}{{"xswap.cmd", false}, {"codex-swap.cmd", false}, {"codex.cmd", true}} {
		path := filepath.Join(directory, item.name)
		if !ownedWindowsWrapper(path, a.Binary, previous.Manager, item.codex) {
			return fmt.Errorf("refusing to replace another command at %s", path)
		}
	}
	if err = writeJSON(filepath.Join(a.Root, "installation.json"), map[string]string{"cli": cli, "manager": a.Binary, "bin": directory}); err != nil {
		return err
	}
	for _, item := range []struct {
		name  string
		codex bool
	}{{"xswap.cmd", false}, {"codex-swap.cmd", false}, {"codex.cmd", true}} {
		if err = atomicWrite(filepath.Join(directory, item.name), windowsWrapper(a.Binary, item.codex)); err != nil {
			return err
		}
	}
	fmt.Println("Installed Windows command wrappers in", directory)
	fmt.Println("Add this directory to your user PATH, then open a new terminal.")
	return nil
}

func (a *App) uninstall() error {
	if err := a.configureAuto(false, 0, 0); err != nil {
		return err
	}
	directory, err := windowsInstallDir()
	if err != nil {
		return err
	}
	for _, item := range []struct {
		name  string
		codex bool
	}{{"xswap.cmd", false}, {"codex-swap.cmd", false}, {"codex.cmd", true}} {
		path := filepath.Join(directory, item.name)
		if ownedWindowsWrapper(path, a.Binary, "", item.codex) {
			if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	fmt.Println("Windows wrappers removed. Accounts and history preserved.")
	return nil
}
