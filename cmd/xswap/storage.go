package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Root, DefaultHome, Binary string
	HTTPClient                *http.Client
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
	return &App{Root: root, DefaultHome: filepath.Join(home, ".codex"), Binary: binary}
}

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

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
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("account %q is not registered; run: xswap add %s", name, name)
		}
	}
	return path, nil
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
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
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
		if entry.IsDir() && validate(entry.Name()) == nil {
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
	if err = os.Mkdir(path, 0700); err != nil {
		return "", fmt.Errorf("account %q already exists or cannot be created; retry login with: xswap login %s", name, name)
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
		directory := filepath.Join(a.Root, "removed")
		if err = os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		archive = filepath.Join(directory, fmt.Sprintf("%s-%d", name, time.Now().UnixNano()))
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
