//go:build !windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	shellBlockStart = "# >>> xswap codex wrapper >>>"
	shellBlockEnd   = "# <<< xswap codex wrapper <<<"
)

type unixInstallation struct {
	CLI        string   `json:"cli"`
	Manager    string   `json:"manager"`
	Bin        string   `json:"bin,omitempty"`
	ShellFiles []string `json:"shellFiles,omitempty"`
}

type shellConfig struct {
	Path string
	Fish bool
}

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

func sameExecutablePath(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	resolvedLeft, leftErr := filepath.EvalSymlinks(left)
	resolvedRight, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(resolvedLeft) == filepath.Clean(resolvedRight)
}

func linkTargets(path, target string) bool {
	link, err := os.Readlink(path)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(path), link)
	}
	return sameExecutablePath(link, target)
}

func shellConfigs(home string) []shellConfig {
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		return []shellConfig{{Path: filepath.Join(home, ".zshrc")}}
	case "fish":
		return []shellConfig{{Path: filepath.Join(home, ".config", "fish", "config.fish"), Fish: true}}
	case "bash":
		configs := []shellConfig{{Path: filepath.Join(home, ".bashrc")}}
		for _, name := range []string{".bash_profile", ".bash_login"} {
			path := filepath.Join(home, name)
			if exists(path) {
				return append(configs, shellConfig{Path: path})
			}
		}
		return append(configs, shellConfig{Path: filepath.Join(home, ".profile")})
	default:
		return []shellConfig{{Path: filepath.Join(home, ".profile")}}
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func shellBlock(directory string, fish bool) string {
	command := "export PATH=" + shellQuote(directory) + ":\"$PATH\""
	if fish {
		command = "fish_add_path --prepend --global " + shellQuote(directory)
	}
	return shellBlockStart + "\n" + command + "\n" + shellBlockEnd
}

func removeShellBlock(data []byte) []byte {
	text := string(data)
	for {
		start := strings.Index(text, shellBlockStart)
		if start < 0 {
			break
		}
		endOffset := strings.Index(text[start:], shellBlockEnd)
		if endOffset < 0 {
			break
		}
		end := start + endOffset + len(shellBlockEnd)
		if end < len(text) && text[end] == '\r' {
			end++
		}
		if end < len(text) && text[end] == '\n' {
			end++
		}
		text = text[:start] + text[end:]
	}
	return []byte(strings.TrimRight(text, "\r\n"))
}

func writeShellConfig(config shellConfig, directory string, remove bool) error {
	mode := os.FileMode(0600)
	data := []byte{}
	info, err := os.Lstat(config.Path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("shell configuration is not a regular file: %s", config.Path)
		}
		mode = info.Mode().Perm()
		data, err = os.ReadFile(config.Path)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if remove {
		return nil
	}
	hasStart := strings.Contains(string(data), shellBlockStart)
	hasEnd := strings.Contains(string(data), shellBlockEnd)
	if hasStart != hasEnd {
		return fmt.Errorf("shell configuration contains an incomplete XSwap block: %s", config.Path)
	}
	data = removeShellBlock(data)
	if !remove {
		if len(data) > 0 {
			data = append(data, '\n', '\n')
		}
		data = append(data, shellBlock(directory, config.Fish)...)
	}
	if len(data) > 0 {
		data = append(data, '\n')
	}
	if err = os.MkdirAll(filepath.Dir(config.Path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(config.Path), ".xswap-shell-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err = temporary.Chmod(mode); err == nil {
		_, err = temporary.Write(data)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary.Name(), config.Path)
}

func shellConfigHasBlock(config shellConfig, directory string) bool {
	data, err := os.ReadFile(config.Path)
	return err == nil && strings.Contains(string(data), shellBlock(directory, config.Fish))
}

func (a *App) durableBin() string {
	return filepath.Join(a.Root, "bin")
}

func (a *App) codexIntegrationIssue() string {
	if _, err := a.original(); err != nil {
		return "Codex integration needs repair; choose Repair Codex integration or run xswap install."
	}
	directory := a.durableBin()
	if !linkTargets(filepath.Join(directory, "codex"), a.Binary) {
		return "Codex integration needs repair; choose Repair Codex integration or run xswap install."
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "Codex integration needs repair; run xswap install."
	}
	for _, config := range shellConfigs(home) {
		if !shellConfigHasBlock(config, directory) {
			return "Codex integration needs repair; choose Repair Codex integration or run xswap install."
		}
	}
	return ""
}

func ownedLink(path string, targets ...string) bool {
	for _, target := range targets {
		if target != "" && linkTargets(path, target) {
			return true
		}
	}
	return false
}

func findOfficialCodex(binary string) (string, error) {
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if directory == "" {
			continue
		}
		candidate := filepath.Join(directory, "codex")
		info, err := os.Stat(candidate)
		if err != nil || !isRunnable(info) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil || sameExecutablePath(resolved, binary) || strings.HasSuffix(resolved, "codex_swap.py") {
			continue
		}
		return resolved, nil
	}
	return "", errors.New("install the official Codex CLI first")
}

func (a *App) install() error {
	var previous unixInstallation
	data, _ := os.ReadFile(filepath.Join(a.Root, "installation.json"))
	_ = json.Unmarshal(data, &previous)
	cli, err := a.original()
	if err != nil {
		cli, err = findOfficialCodex(a.Binary)
		if err != nil {
			return err
		}
	}
	home, _ := os.UserHomeDir()
	legacyDirectory := filepath.Join(home, ".local", "bin")
	durableDirectory := a.durableBin()
	if err = os.MkdirAll(legacyDirectory, 0755); err != nil {
		return err
	}
	if err = os.MkdirAll(durableDirectory, 0700); err != nil {
		return err
	}
	for _, name := range []string{"codex-swap", "xswap"} {
		path := filepath.Join(legacyDirectory, name)
		if info, statErr := os.Lstat(path); statErr == nil && (info.Mode()&os.ModeSymlink == 0 || !ownedLink(path, a.Binary, previous.Manager, filepath.Join(filepath.Dir(a.Binary), "codex-swap"))) {
			return fmt.Errorf("refusing to replace another executable at %s", path)
		}
	}
	for _, name := range []string{"codex-swap", "xswap", "codex"} {
		path := filepath.Join(durableDirectory, name)
		if info, statErr := os.Lstat(path); statErr == nil && (info.Mode()&os.ModeSymlink == 0 || !ownedLink(path, a.Binary, previous.Manager)) {
			return fmt.Errorf("refusing to replace another executable at %s", path)
		}
	}
	configs := shellConfigs(home)
	currentConfigs := map[string]bool{}
	for _, config := range configs {
		currentConfigs[config.Path] = true
	}
	for _, path := range previous.ShellFiles {
		if !currentConfigs[path] {
			if err = writeShellConfig(shellConfig{Path: path, Fish: filepath.Base(path) == "config.fish"}, durableDirectory, true); err != nil {
				return err
			}
		}
	}
	for _, config := range configs {
		if err = writeShellConfig(config, durableDirectory, false); err != nil {
			return err
		}
	}
	metadata := unixInstallation{CLI: cli, Manager: a.Binary, Bin: durableDirectory}
	for _, config := range configs {
		metadata.ShellFiles = append(metadata.ShellFiles, config.Path)
	}
	if err = writeJSON(filepath.Join(a.Root, "installation.json"), metadata); err != nil {
		return err
	}
	backup := filepath.Join(a.Root, "original-codex")
	if _, err = os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
		if err = os.Symlink(cli, backup); err != nil {
			return err
		}
	}
	for _, name := range []string{"codex-swap", "xswap", "codex"} {
		if err = replaceLink(filepath.Join(durableDirectory, name), a.Binary); err != nil {
			return err
		}
	}
	for _, name := range []string{"codex-swap", "xswap"} {
		if err = replaceLink(filepath.Join(legacyDirectory, name), a.Binary); err != nil {
			return err
		}
	}
	legacyCodex := filepath.Join(legacyDirectory, "codex")
	if info, statErr := os.Lstat(legacyCodex); errors.Is(statErr, os.ErrNotExist) || (statErr == nil && info.Mode()&os.ModeSymlink != 0 && ownedLink(legacyCodex, a.Binary, previous.Manager, cli)) {
		if err = replaceLink(legacyCodex, a.Binary); err != nil {
			return err
		}
	}
	fmt.Println("Installed durable XSwap wrappers in", durableDirectory)
	fmt.Println("Updated your shell PATH. Open a new terminal to keep Codex updates from replacing the XSwap wrapper.")
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
	var metadata unixInstallation
	_ = readJSON(filepath.Join(a.Root, "installation.json"), &metadata)
	home, _ := os.UserHomeDir()
	legacyDirectory := filepath.Join(home, ".local", "bin")
	durableDirectory := metadata.Bin
	if durableDirectory == "" {
		durableDirectory = a.durableBin()
	}
	for _, path := range metadata.ShellFiles {
		fish := filepath.Base(path) == "config.fish"
		if err = writeShellConfig(shellConfig{Path: path, Fish: fish}, durableDirectory, true); err != nil {
			return err
		}
	}
	for _, name := range []string{"codex-swap", "xswap", "codex"} {
		path := filepath.Join(durableDirectory, name)
		if ownedLink(path, a.Binary, metadata.Manager) {
			if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	_ = os.Remove(durableDirectory)
	for _, name := range []string{"codex-swap", "xswap"} {
		path := filepath.Join(legacyDirectory, name)
		if ownedLink(path, a.Binary, metadata.Manager) {
			if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	legacyCodex := filepath.Join(legacyDirectory, "codex")
	if ownedLink(legacyCodex, a.Binary, metadata.Manager) {
		if err = replaceLink(legacyCodex, cli); err != nil {
			return err
		}
	}
	fmt.Println("Wrappers and shell PATH integration removed. Accounts and history preserved.")
	return nil
}
