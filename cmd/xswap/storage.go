package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type App struct {
	Root, DefaultHome, Binary, PackageManager string
	HTTPClient                                *http.Client
	CommandRunner                             func(context.Context, string, ...string) error
	ServiceRunner                             func(context.Context, string, ...string) (string, error)
	SessionIndexer                            func(string, string) error
	SessionActive                             func(string, string) (bool, error)
	PanelActionRunner                         func(string, io.Reader, io.Writer) panelActionResult
}
type AutoConfig struct {
	Enabled   bool `json:"enabled"`
	Threshold int  `json:"threshold"`
	Interval  int  `json:"interval"`
	Cooldown  int  `json:"cooldown"`
	Margin    int  `json:"margin"`
}
type Settings struct {
	Disabled     []string          `json:"disabled"`
	DisplayNames map[string]string `json:"displayNames,omitempty"`
	Auto         AutoConfig        `json:"auto"`
}

func newApp() *App {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	root := os.Getenv("CODEX_SWAP_HOME")
	if root == "" {
		root = filepath.Join(home, ".codex-swap")
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	binary, _ := os.Executable()
	binary, _ = filepath.EvalSymlinks(binary)
	packageManager := strings.ToLower(strings.TrimSpace(os.Getenv("XSWAP_PACKAGE_MANAGER")))
	if packageManager != "homebrew" {
		packageManager = ""
	}
	if managed := os.Getenv("XSWAP_EXECUTABLE"); packageManager != "" && filepath.IsAbs(managed) {
		binary = filepath.Clean(managed)
	}
	return &App{Root: root, DefaultHome: filepath.Join(home, ".codex"), Binary: binary, PackageManager: packageManager}
}

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

const profileMarker = ".xswap-profile"

func validate(name string) error {
	if !validName.MatchString(name) {
		return errors.New("invalid account name: use up to 64 letters, numbers, dots, _ or -")
	}
	return nil
}
func exists(path string) bool { _, err := os.Stat(path); return err == nil }
func (a *App) profile(name string) (string, error) {
	if err := validate(name); err != nil {
		return "", err
	}
	if name == "default" {
		return a.DefaultHome, nil
	}
	return filepath.Join(a.Root, "profiles", name), nil
}
func (a *App) require(name string) (string, error) {
	path, err := a.profile(name)
	if err != nil {
		return "", err
	}
	if name != "default" {
		if !registeredProfile(path) {
			return "", fmt.Errorf("account %q is not registered; run: xswap add %s", name, name)
		}
	}
	return path, nil
}

// registeredProfile recognizes current XSwap profiles and legacy profiles that
// predate the marker. Codex may recreate an old CODEX_HOME directory after its
// account was removed; that directory must not become a registered account.
func registeredProfile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if regularFile(filepath.Join(path, profileMarker)) {
		return true
	}
	if regularFile(filepath.Join(path, "auth.json")) || regularFile(filepath.Join(path, "config.toml")) {
		return true
	}
	info, err = os.Lstat(filepath.Join(path, "skills"))
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
func (a *App) selected() string {
	data, err := os.ReadFile(filepath.Join(a.Root, "active"))
	if err != nil {
		return "default"
	}
	name := stringTrim(data)
	if validate(name) != nil {
		return "default"
	}
	return name
}
func stringTrim(data []byte) string { return string(bytesTrim(data)) }
func bytesTrim(data []byte) []byte {
	for len(data) > 0 && (data[len(data)-1] == '\n' || data[len(data)-1] == '\r' || data[len(data)-1] == ' ') {
		data = data[:len(data)-1]
	}
	return data
}
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".swap-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'))
}
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
func (a *App) settings() (Settings, error) {
	s := Settings{Disabled: []string{}, DisplayNames: map[string]string{}, Auto: AutoConfig{false, 90, 60, 300, 5}}
	data, err := os.ReadFile(filepath.Join(a.Root, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	if s.DisplayNames == nil {
		s.DisplayNames = map[string]string{}
	}
	if err == nil && (s.Auto.Threshold < 1 || s.Auto.Threshold > 100 || s.Auto.Interval < 10 || s.Auto.Cooldown < 0 || s.Auto.Margin < 0) {
		err = errors.New("invalid auto-switch configuration")
	}
	return s, err
}
func (a *App) displayName(name string) string {
	s, err := a.settings()
	if err == nil && s.DisplayNames != nil {
		if label := strings.TrimSpace(s.DisplayNames[name]); label != "" {
			return label
		}
	}
	return name
}
func (a *App) setDisplayName(name, label string) error {
	if _, err := a.require(name); err != nil {
		return err
	}
	label = strings.TrimSpace(label)
	if len([]rune(label)) > 64 {
		return errors.New("display name must be 64 characters or fewer")
	}
	return a.withState(func() error {
		s, err := a.settings()
		if err != nil {
			return err
		}
		if s.DisplayNames == nil {
			s.DisplayNames = map[string]string{}
		}
		if label == "" {
			delete(s.DisplayNames, name)
		} else {
			s.DisplayNames[name] = label
		}
		return writeJSON(filepath.Join(a.Root, "settings.json"), s)
	})
}
func disabled(s Settings, name string) bool {
	for _, item := range s.Disabled {
		if item == name {
			return true
		}
	}
	return false
}
func (a *App) lock(ctx context.Context, name string) (func(), error) {
	path := filepath.Join(a.Root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lockPath := path + ".lockdir"
	for {
		err := os.Mkdir(lockPath, 0700)
		if err == nil {
			owner := filepath.Join(lockPath, "owner")
			if err := os.WriteFile(owner, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
				_ = os.Remove(lockPath)
				return nil, err
			}
			return func() {
				_ = os.Remove(owner)
				_ = os.Remove(lockPath)
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if reclaimAbandonedLock(lockPath) {
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func reclaimAbandonedLock(lockPath string) bool {
	info, err := os.Stat(lockPath)
	if err != nil || !info.IsDir() || time.Since(info.ModTime()) < 2*time.Second {
		return false
	}
	owner := filepath.Join(lockPath, "owner")
	data, err := os.ReadFile(owner)
	pid, parseErr := strconv.Atoi(string(data))
	legacy := err != nil || parseErr != nil || pid <= 0
	if legacy && time.Since(info.ModTime()) < 24*time.Hour {
		return false
	}
	if !legacy && managedProcessAlive(pid) {
		return false
	}
	reaper := filepath.Join(lockPath, "reaper")
	if os.Mkdir(reaper, 0700) != nil {
		return false
	}
	claimed := true
	defer func() {
		if claimed {
			_ = os.Remove(reaper)
		}
	}()
	// Recheck after claiming the directory: another contender may have recovered it.
	current, err := os.Stat(lockPath)
	if err != nil || !os.SameFile(info, current) {
		return false
	}
	data, err = os.ReadFile(owner)
	pid, parseErr = strconv.Atoi(string(data))
	if err == nil && parseErr == nil && pid > 0 && managedProcessAlive(pid) {
		return false
	}
	if err == nil && os.Remove(owner) != nil {
		return false
	}
	if os.Remove(reaper) != nil {
		return false
	}
	claimed = false
	return os.Remove(lockPath) == nil
}
func (a *App) withState(fn func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := a.lock(ctx, "state.lock")
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}
func (a *App) names() []string {
	names := []string{"default"}
	entries, _ := os.ReadDir(filepath.Join(a.Root, "profiles"))
	for _, entry := range entries {
		if validate(entry.Name()) == nil && registeredProfile(filepath.Join(a.Root, "profiles", entry.Name())) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names[1:])
	return names
}
func (a *App) setEnabled(name string, enabled bool) error {
	if _, err := a.require(name); err != nil {
		return err
	}
	return a.withState(func() error {
		s, err := a.settings()
		if err != nil {
			return err
		}
		next := []string{}
		for _, n := range s.Disabled {
			if n != name {
				next = append(next, n)
			}
		}
		if !enabled {
			next = append(next, name)
		}
		sort.Strings(next)
		s.Disabled = next
		return writeJSON(filepath.Join(a.Root, "settings.json"), s)
	})
}
func (a *App) selectAccount(name string) error {
	return a.withState(func() error {
		path, err := a.require(name)
		if err != nil {
			return err
		}
		if name != "default" && !exists(filepath.Join(path, "auth.json")) {
			return fmt.Errorf("login pending; run: xswap login %s", name)
		}
		if err := atomicWrite(filepath.Join(a.Root, "active"), []byte(name+"\n")); err != nil {
			return err
		}
		return atomicWrite(filepath.Join(a.Root, "last-switch"), []byte(strconv.FormatInt(time.Now().Unix(), 10)))
	})
}
func (a *App) create(name string) (string, error) {
	if name == "default" {
		return "", errors.New("default is reserved for your original account")
	}
	path, err := a.profile(name)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("account path %q exists but is not a registered profile", name)
		}
		if registeredProfile(path) {
			return "", fmt.Errorf("account %q already exists; retry login with: xswap login %s", name, name)
		}
		archive, archiveErr := a.archivePath(name)
		if archiveErr != nil {
			return "", archiveErr
		}
		if err = os.Rename(path, archive); err != nil {
			return "", fmt.Errorf("archive leftover data for account %q: %w", name, err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	if err = os.Mkdir(path, 0700); err != nil {
		return "", fmt.Errorf("account %q already exists or cannot be created; retry login with: xswap login %s", name, name)
	}
	if err = atomicWrite(filepath.Join(path, profileMarker), []byte("1\n")); err != nil {
		return "", err
	}
	if config, err := os.ReadFile(filepath.Join(a.DefaultHome, "config.toml")); err == nil {
		if err = atomicWrite(filepath.Join(path, "config.toml"), config); err != nil {
			return "", err
		}
	}
	skills := filepath.Join(a.DefaultHome, "skills")
	if exists(skills) {
		if err = os.Symlink(skills, filepath.Join(path, "skills")); err != nil {
			return "", err
		}
	}
	return name, nil
}
func (a *App) createNumbered() (string, error) {
	for number := 1; ; number++ {
		name := fmt.Sprintf("account-%d", number)
		path, _ := a.profile(name)
		if !exists(path) {
			return a.create(name)
		}
	}
}
func (a *App) remove(name string) (string, error) {
	if name == "default" {
		return "", errors.New("default is protected; disable it to exclude it from rotation")
	}
	if err := validate(name); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Second)
	defer cancel()
	unlock, err := a.lock(ctx, filepath.Join("locks", name+".lock"))
	if err != nil {
		return "", err
	}
	defer unlock()
	var archive string
	err = a.withState(func() error {
		home, err := a.require(name)
		if err != nil {
			return err
		}
		if a.selected() == name {
			return errors.New("switch to another account before removing the active account")
		}
		info, err := os.Lstat(home)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to remove a symlink profile")
		}
		s, err := a.settings()
		if err != nil {
			return err
		}
		archive, err = a.archivePath(name)
		if err != nil {
			return err
		}
		if err = os.Rename(home, archive); err != nil {
			return err
		}
		next := []string{}
		for _, n := range s.Disabled {
			if n != name {
				next = append(next, n)
			}
		}
		s.Disabled = next
		return writeJSON(filepath.Join(a.Root, "settings.json"), s)
	})
	return archive, err
}

func (a *App) archivePath(name string) (string, error) {
	directory := filepath.Join(a.Root, "removed")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	return filepath.Join(directory, fmt.Sprintf("%s-%d", name, time.Now().UnixNano())), nil
}
