package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const projectAccountFile = ".xswap-account"

type projectSelection struct {
	Account string
	File    string
}

func projectRoot(start string) (string, error) {
	root, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		root = filepath.Dir(root)
	}
	for current := root; ; current = filepath.Dir(current) {
		if _, err = os.Lstat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return root, nil
		}
	}
}

func (a *App) projectSelection(start string) (projectSelection, bool, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return projectSelection{}, false, err
	}
	if info, statErr := os.Stat(current); statErr == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	for {
		path := filepath.Join(current, projectAccountFile)
		info, statErr := os.Lstat(path)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return projectSelection{}, false, fmt.Errorf("project account file must be a regular file: %s", path)
			}
			if info.Size() > 256 {
				return projectSelection{}, false, fmt.Errorf("project account file is too large: %s", path)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return projectSelection{}, false, readErr
			}
			name := strings.TrimSpace(string(data))
			if err = validate(name); err != nil {
				return projectSelection{}, false, fmt.Errorf("invalid project account in %s: %w", path, err)
			}
			if _, err = a.require(name); err != nil {
				return projectSelection{}, false, fmt.Errorf("project account in %s is unavailable: %w", path, err)
			}
			settings, settingsErr := a.settings()
			if settingsErr != nil {
				return projectSelection{}, false, settingsErr
			}
			if disabled(settings, name) {
				return projectSelection{}, false, fmt.Errorf("project account %q is disabled", name)
			}
			return projectSelection{Account: name, File: path}, true, nil
		}
		if !os.IsNotExist(statErr) {
			return projectSelection{}, false, statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return projectSelection{}, false, nil
		}
		current = parent
	}
}

func (a *App) accountForDirectory(directory string) (string, error) {
	selection, found, err := a.projectSelection(directory)
	if err != nil {
		return "", err
	}
	if found {
		return selection.Account, nil
	}
	return a.selected(), nil
}

func (a *App) pinProject(directory, name string) (string, error) {
	if _, err := a.require(name); err != nil {
		return "", err
	}
	settings, err := a.settings()
	if err != nil {
		return "", err
	}
	if disabled(settings, name) {
		return "", fmt.Errorf("account %q is disabled", name)
	}
	root, err := projectRoot(directory)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, projectAccountFile)
	if info, statErr := os.Lstat(path); statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return "", fmt.Errorf("refusing to replace non-regular project account file: %s", path)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}
	if err = atomicWrite(path, []byte(name+"\n")); err != nil {
		return "", err
	}
	return root, nil
}

func clearProject(directory string) (string, error) {
	root, err := projectRoot(directory)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, projectAccountFile)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return root, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("refusing to remove a non-regular project account file")
	}
	return root, os.Remove(path)
}
