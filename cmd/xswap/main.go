package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	a := newApp()
	var err error
	if filepath.Base(os.Args[0]) == "codex" {
		a.ensureDaemon()
		err = a.launch(a.selected(), os.Args[1:], true)
	} else {
		err = a.run(os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "xswap:", err)
		os.Exit(1)
	}
}

var version = "dev"
var commit = "unknown"
var buildDate = "unknown"
var releaseRepo = ""

func usage() {
	fmt.Print(`XSwap — account manager for the official Codex CLI

  xswap                         Open the account menu
  xswap add [NAME]               Add an account and open browser login
  xswap rename NAME [--label X]  Set or clear an account display name
  xswap login NAME               Retry login (--device-auth supported)
  xswap list                    List accounts
  xswap switch NAME             Select an account for new Codex processes
  xswap run NAME -- [ARGS]       Run an account without changing selection
  xswap status [NAME]            Check official Codex login status
  xswap limits [NAME] [--all]    Show current quota usage and resets
  xswap watch [NAME]             Watch quota usage (--interval 30, --once)
  xswap auto on                 Start automatic rotation in the background
  xswap auto off                Stop automatic rotation
  xswap auto status             Show monitor status
  xswap auto view               Open the auto-switch screen
  xswap auto --once --dry-run    Preview a single rotation check
  xswap disable NAME            Exclude account from automatic rotation
  xswap enable NAME             Include account in automatic rotation
  xswap remove NAME             Remove account and archive its local data
  xswap install                 Install or repair command wrappers
  xswap uninstall               Restore the original Codex command
  xswap version                 Show version, commit, and build date
  xswap update --check          Check published GitHub releases
  xswap update                  Install a release after confirmation

Auto-switch changes the account used by NEW Codex processes.
Existing processes keep the account with which they were started.
`)
}

type Options struct {
	Names  []string
	Flags  map[string]bool
	Values map[string]string
}

func parse(args []string) (Options, error) {
	o := Options{Flags: map[string]bool{}, Values: map[string]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--help" || arg == "-h" {
			o.Flags["help"] = true
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			o.Names = append(o.Names, arg)
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		switch key {
		case "device-auth", "all", "yes", "once", "dry-run", "check":
			o.Flags[key] = true
		case "threshold", "interval", "repo", "label":
			i++
			if i >= len(args) {
				return o, fmt.Errorf("--%s requires a value", key)
			}
			o.Values[key] = args[i]
		default:
			return o, fmt.Errorf("unknown option: %s", arg)
		}
	}
	return o, nil
}
func (o Options) integer(key string, fallback int) (int, error) {
	value, ok := o.Values[key]
	if !ok {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("--%s requires an integer", key)
	}
	return number, nil
}
func (a *App) run(args []string) error {
	if len(args) == 0 {
		if interactive() {
			return a.dashboard("home", "", 60)
		}
		return a.list()
	}
	action := args[0]
	if action == "__codex" {
		a.ensureDaemon()
		return a.launch(a.selected(), args[1:], true)
	}
	if action == "__daemon" {
		return a.daemon()
	}
	if action == "help" || action == "--help" || action == "-h" {
		usage()
		return nil
	}
	if action == "run" {
		if len(args) < 2 {
			return errors.New("usage: xswap run NAME -- [ARGS]")
		}
		extra := args[2:]
		if len(extra) > 0 && extra[0] == "--" {
			extra = extra[1:]
		}
		return a.launch(args[1], extra, false)
	}
	o, err := parse(args[1:])
	if err != nil {
		return err
	}
	if o.Flags["help"] {
		usage()
		return nil
	}
	if len(o.Names) > 1 {
		return errors.New("expected at most one account name or operation")
	}
	name := ""
	if len(o.Names) > 0 {
		name = o.Names[0]
	}
	switch action {
	case "update":
		_, err := a.updateCommand(o)
		return err
	case "version":
		fmt.Printf("XSwap %s\nCommit: %s\nBuilt: %s\n", version, commit, buildDate)
		return nil
	case "install":
		return a.install()
	case "uninstall":
		return a.uninstall()
	case "list":
		return a.list()
	case "current":
		fmt.Println(a.selected())
		return nil
	case "add":
		if name == "" {
			name, err = a.createNumbered()
		} else {
			name, err = a.create(name)
		}
		if err != nil {
			return err
		}
		fmt.Printf("Adding %s. Sign in to the account you want to register.\n", name)
		if label := o.Values["label"]; label != "" {
			if err = a.setDisplayName(name, label); err != nil {
				return err
			}
		}
		return a.login(name, o.Flags["device-auth"])
	case "rename":
		if name == "" {
			return errors.New("usage: xswap rename NAME --label LABEL")
		}
		if _, ok := o.Values["label"]; !ok {
			return errors.New("usage: xswap rename NAME --label LABEL (use an empty label to clear)")
		}
		if err = a.setDisplayName(name, o.Values["label"]); err != nil {
			return err
		}
		if o.Values["label"] == "" {
			fmt.Printf("Display name cleared for %s.\n", name)
		} else {
			fmt.Printf("Display name for %s: %s\n", name, strings.TrimSpace(o.Values["label"]))
		}
		return nil
	case "login":
		if name == "" {
			return errors.New("usage: xswap login NAME")
		}
		return a.login(name, o.Flags["device-auth"])
	case "switch":
		if name == "" {
			return errors.New("usage: xswap switch NAME")
		}
		if err = a.selectAccount(name); err != nil {
			return err
		}
		fmt.Printf("Selected: %s. New Codex processes will use this account.\n", name)
		if os.Getenv("CODEX_HOME") != "" {
			fmt.Println("Explicit CODEX_HOME takes priority; unset CODEX_HOME to use this selection.")
		}
		return nil
	case "status":
		if name == "" {
			name = a.selected()
		}
		return a.launch(name, []string{"login", "status"}, false)
	case "disable", "enable":
		if name == "" {
			return fmt.Errorf("usage: xswap %s NAME", action)
		}
		if err = a.setEnabled(name, action == "enable"); err != nil {
			return err
		}
		fmt.Printf("%s: %sd for automatic rotation. Manual selection remains available.\n", name, action)
		return nil
	case "remove":
		if name == "" {
			return errors.New("usage: xswap remove NAME")
		}
		if _, err = a.require(name); err != nil {
			return err
		}
		if !o.Flags["yes"] {
			if !interactive() {
				return errors.New("use --yes to confirm removal in scripts")
			}
			fmt.Printf("Remove %s from the list and archive its data? [y/N] ", name)
			answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("Removal cancelled.")
				return nil
			}
		}
		archive, err := a.remove(name)
		if err != nil {
			return err
		}
		fmt.Println("Account removed. Data preserved at", archive)
		return nil
	case "limits", "usage":
		names := []string{name}
		if name == "" {
			names = []string{a.selected()}
		}
		if o.Flags["all"] {
			if name != "" {
				return errors.New("choose NAME or --all")
			}
			names = a.names()
		}
		return a.showLimits(context.Background(), names)
	case "watch":
		interval, err := o.integer("interval", 60)
		if err != nil {
			return err
		}
		if interval < 10 {
			return errors.New("minimum refresh interval is 10 seconds")
		}
		if name != "" {
			if _, err = a.require(name); err != nil {
				return err
			}
		}
		if o.Flags["once"] {
			names := []string{name}
			if name == "" {
				names = a.names()
			}
			return a.showLimits(context.Background(), names)
		}
		return a.dashboard("watch", name, interval)
	case "auto":
		return a.autoCommand(name, o)
	default:
		return fmt.Errorf("unknown command: %s; run xswap --help", action)
	}
}
func (a *App) list() error {
	s, err := a.settings()
	if err != nil {
		return err
	}
	for _, name := range a.names() {
		marker := " "
		if name == a.selected() {
			marker = "*"
		}
		note := "original account"
		home, _ := a.profile(name)
		if name != "default" {
			note = "login pending"
			if exists(filepath.Join(home, "auth.json")) {
				note = "login saved"
			}
		}
		if disabled(s, name) {
			note += " · disabled for rotation"
		}
		label := name
		if custom := strings.TrimSpace(s.DisplayNames[name]); custom != "" {
			label = custom
		}
		fmt.Printf("%s %s: %s\n", marker, label, note)
	}
	fmt.Println("\nadd | switch | watch | auto | disable | enable | remove | --help")
	return nil
}
