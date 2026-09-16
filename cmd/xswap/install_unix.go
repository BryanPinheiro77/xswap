//go:build !windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func replaceLink(path, target string) error {
	tmp := path + ".xswap-install"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (a *App) install() error {
	var previous struct {
		Manager string `json:"manager"`
	}
	data, _ := os.ReadFile(filepath.Join(a.Root, "installation.json"))
	json.Unmarshal(data, &previous)
	cli, err := a.original()
	if err != nil {
		found, lookupErr := exec.LookPath("codex")
		if lookupErr != nil {
			return errors.New("install the official Codex CLI first")
		}
		cli, err = filepath.EvalSymlinks(found)
		if err != nil {
			return err
		}
		if cli == a.Binary || strings.HasSuffix(cli, "codex_swap.py") {
			return errors.New("cannot locate the original Codex CLI")
		}
	}
	home, _ := os.UserHomeDir()
	directory := filepath.Join(home, ".local", "bin")
	if err = os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	for _, name := range []string{"codex-swap", "xswap", "codex"} {
		path := filepath.Join(directory, name)
		info, statErr := os.Lstat(path)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("refusing to replace another executable at %s", path)
			}
			target, _ := filepath.EvalSymlinks(path)
			link, _ := os.Readlink(path)
			legacy := filepath.Join(filepath.Dir(a.Binary), "codex-swap")
			if target != a.Binary && target != cli && target != legacy && (previous.Manager == "" || link != previous.Manager) && !strings.HasSuffix(target, "/codex_swap.py") {
				return fmt.Errorf("refusing to replace another program at %s", path)
			}
		}
	}
	if err = writeJSON(filepath.Join(a.Root, "installation.json"), map[string]string{"cli": cli, "manager": a.Binary}); err != nil {
		return err
	}
	backup := filepath.Join(a.Root, "original-codex")
	if _, err = os.Lstat(backup); os.IsNotExist(err) {
		if err = os.Symlink(cli, backup); err != nil {
			return err
		}
	}
	for _, name := range []string{"codex-swap", "xswap", "codex"} {
		if err = replaceLink(filepath.Join(directory, name), a.Binary); err != nil {
			return err
		}
	}
	fmt.Println("Installed Go executable: xswap / codex-swap. codex uses the selected account.")
	return nil
}

func (a *App) uninstall() error {
	cli, err := a.original()
	if err != nil {
		return err
	}
	if err = a.configureAuto(false, 0, 0); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	directory := filepath.Join(home, ".local", "bin")
	for _, name := range []string{"codex-swap", "xswap", "codex"} {
		path := filepath.Join(directory, name)
		target, _ := filepath.EvalSymlinks(path)
		if target != a.Binary {
			continue
		}
		if name == "codex" {
			if err = replaceLink(path, cli); err != nil {
				return err
			}
		} else if err = os.Remove(path); err != nil {
			return err
		}
	}
	fmt.Println("Wrappers removed. Accounts and history preserved.")
	return nil
}
