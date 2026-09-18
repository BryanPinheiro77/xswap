package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const projectAccountFile = ".xswap-account"
const projectAccountExclude = "/.xswap-account"

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

func isHomeScope(root string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return filepath.Clean(root) == filepath.Clean(home)
}

func validateProjectPinScope(root string) error {
	clean := filepath.Clean(root)
	if isHomeScope(clean) || filepath.Dir(clean) == clean {
		return errors.New("project pins cannot be created at the user home or filesystem root")
	}
	return nil
}

func validateHandoffScope(root string) error {
	clean := filepath.Clean(root)
	if filepath.Dir(clean) == clean {
		return errors.New("session handoff cannot use the filesystem root")
	}
	return nil
}

func gitMetadataDirectory(root string) (string, bool, error) {
	path := filepath.Join(root, ".git")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	gitDir := path
	if !info.IsDir() {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 4096 {
			return "", false, errors.New(".git must be a directory or a regular gitdir file")
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", false, readErr
		}
		value := strings.TrimSpace(string(data))
		if !strings.HasPrefix(value, "gitdir:") {
			return "", false, errors.New(".git file has an invalid gitdir reference")
		}
		gitDir = strings.TrimSpace(strings.TrimPrefix(value, "gitdir:"))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(root, gitDir)
		}
	}
	gitDir, err = filepath.Abs(gitDir)
	if err != nil {
		return "", false, err
	}
	if info, err = os.Stat(gitDir); err != nil || !info.IsDir() {
		return "", false, errors.New("git metadata directory is unavailable")
	}
	commonPath := filepath.Join(gitDir, "commondir")
	if commonInfo, commonErr := os.Lstat(commonPath); commonErr == nil {
		if commonInfo.Mode()&os.ModeSymlink != 0 || !commonInfo.Mode().IsRegular() || commonInfo.Size() > 4096 {
			return "", false, errors.New("git commondir reference is invalid")
		}
		data, readErr := os.ReadFile(commonPath)
		if readErr != nil {
			return "", false, readErr
		}
		common := strings.TrimSpace(string(data))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		gitDir, err = filepath.Abs(common)
		if err != nil {
			return "", false, err
		}
	} else if !os.IsNotExist(commonErr) {
		return "", false, commonErr
	}
	if info, err = os.Stat(gitDir); err != nil || !info.IsDir() {
		return "", false, errors.New("git common metadata directory is unavailable")
	}
	return gitDir, true, nil
}

func ensureProjectAccountExcluded(root string) error {
	gitDir, found, err := gitMetadataDirectory(root)
	if err != nil || !found {
		return err
	}
	path := filepath.Join(gitDir, "info", "exclude")
	data := []byte{}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return errors.New("git info/exclude must be a small regular file")
		}
		data, err = os.ReadFile(path)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == projectAccountExclude {
			return nil
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, []byte("# XSwap local account selection\n"+projectAccountExclude+"\n")...)
	return atomicWrite(path, data)
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
	if err = validateProjectPinScope(root); err != nil {
		return "", err
	}
	if err = ensureProjectAccountExcluded(root); err != nil {
		return "", fmt.Errorf("protect project account from Git tracking: %w", err)
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
