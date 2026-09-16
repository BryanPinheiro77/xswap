//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
)

func (a *App) replaceInstalledBinary(binary []byte, _ string) error {
	if a.Binary == "" {
		return errors.New("installed executable location unavailable")
	}
	old, err := os.ReadFile(a.Binary)
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(a.Root, "previous-xswap"), old); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(a.Binary), ".xswap-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(binary); err == nil {
		err = tmp.Chmod(0755)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), a.Binary)
}
