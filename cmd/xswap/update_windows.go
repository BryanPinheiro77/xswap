//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type windowsInstallation struct {
	CLI     string `json:"cli"`
	Manager string `json:"manager"`
	Bin     string `json:"bin"`
}

func (a *App) replaceInstalledBinary(binary []byte, tag string) error {
	if a.Binary == "" {
		return errors.New("installed executable location unavailable")
	}
	var installation windowsInstallation
	if err := readJSON(filepath.Join(a.Root, "installation.json"), &installation); err != nil {
		return errors.New("Windows updates require an installed XSwap; run: xswap install")
	}
	if !strings.EqualFold(filepath.Clean(installation.Manager), filepath.Clean(a.Binary)) {
		return errors.New("installation metadata does not match the running XSwap executable; run: xswap install")
	}
	directory, err := windowsInstallDir()
	if err != nil {
		return err
	}
	if installation.Bin == "" || !strings.EqualFold(filepath.Clean(installation.Bin), filepath.Clean(directory)) {
		return errors.New("installation metadata has an invalid Windows command directory; run: xswap install")
	}
	for _, item := range windowsCommands {
		path := filepath.Join(directory, item.name)
		if !ownedWindowsWrapper(path, a.Binary, "", item.codex) {
			return fmt.Errorf("refusing to replace another command at %s", path)
		}
	}
	old, err := os.ReadFile(a.Binary)
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(a.Root, "previous-xswap"), old); err != nil {
		return err
	}
	appDirectory := filepath.Join(filepath.Dir(directory), "app")
	if err = os.MkdirAll(appDirectory, 0700); err != nil {
		return err
	}
	target := filepath.Join(appDirectory, "xswap-"+tag+".exe")
	created := false
	if existing, readErr := os.ReadFile(target); readErr == nil {
		if !bytes.Equal(existing, binary) {
			return errors.New("a different executable already exists for this XSwap version")
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	} else {
		if err = atomicWrite(target, binary); err != nil {
			return err
		}
		created = true
	}
	rollback := func() {
		for _, item := range windowsCommands {
			_ = atomicWrite(filepath.Join(directory, item.name), windowsWrapper(a.Binary, item.codex))
		}
		if created {
			_ = os.Remove(target)
		}
	}
	for _, item := range windowsCommands {
		if err = atomicWrite(filepath.Join(directory, item.name), windowsWrapper(target, item.codex)); err != nil {
			rollback()
			return err
		}
	}
	installation.Manager = target
	if err = writeJSON(filepath.Join(a.Root, "installation.json"), installation); err != nil {
		rollback()
		return err
	}
	a.Binary = target
	return nil
}
