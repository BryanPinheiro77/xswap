package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	a := newApp()
	var err error
	if filepath.Base(os.Args[0]) == "codex" {
		a.ensureDaemon()
		name := a.selected()
		if os.Getenv("CODEX_HOME") == "" {
			name, err = a.accountForDirectory(currentDirectory())
		}
		if err == nil {
			err = a.superviseCodex(name, os.Args[1:])
		}
	} else {
		err = a.run(os.Args[1:])
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			if code < 1 {
				code = 1
			}
			os.Exit(code)
		}
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
  xswap project use NAME        Pin an account to the current project
  xswap project switch NAME     Copy this project's sessions and switch account
  xswap project clear           Remove the current project's account pin
  xswap project current         Show the effective account for this directory
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
  xswap update                  Install the latest update after confirmation

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
		case "threshold", "interval", "repo", "label", "path":
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
		name := a.selected()
		if os.Getenv("CODEX_HOME") == "" {
			var accountErr error
			name, accountErr = a.accountForDirectory(currentDirectory())
			if accountErr != nil {
				return accountErr
			}
		}
		return a.superviseCodex(name, args[1:])
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
	if action == "project" {
		return a.projectCommand(args[1:])
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
		if a.PackageManager == "homebrew" {
			fmt.Println("Managed by: Homebrew")
		}
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

func currentDirectory() string {
	directory, err := os.Getwd()
	if err != nil {
		return "."
	}
	return directory
}

func (a *App) projectCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xswap project use NAME | switch NAME | clear | current")
	}
	o, err := parse(args[1:])
	if err != nil {
		return err
	}
	directory := o.Values["path"]
	if directory == "" {
		directory = currentDirectory()
	}
	switch args[0] {
	case "use":
		if len(o.Names) != 1 {
			return errors.New("usage: xswap project use NAME [--path DIR]")
		}
		root, pinErr := a.pinProject(directory, o.Names[0])
		if pinErr != nil {
			return pinErr
		}
		fmt.Printf("Project %s will use account %s for new Codex processes.\n", root, o.Names[0])
		return nil
	case "switch":
		if len(o.Names) != 1 {
			return errors.New("usage: xswap project switch NAME [--path DIR] [--yes]")
		}
		plan, planErr := a.planProjectHandoff(directory, o.Names[0])
		if planErr != nil {
			return planErr
		}
		if !o.Flags["yes"] {
			if !interactive() {
				return errors.New("use --yes to confirm a project account switch in scripts")
			}
			fmt.Printf("Switch %s from %s to %s, copy %d conversation(s), and restart %d managed session(s)? [y/N] ", plan.Project, plan.Source, plan.Target, len(plan.Sessions), len(plan.Managed))
			answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("Project switch cancelled.")
				return nil
			}
		}
		result, handoffErr := a.requestProjectHandoff(plan)
		if handoffErr != nil {
			return handoffErr
		}
		printProjectHandoffResult(a, result)
		return nil
	case "clear":
		if len(o.Names) != 0 {
			return errors.New("usage: xswap project clear [--path DIR]")
		}
		root, clearErr := clearProject(directory)
		if clearErr != nil {
			return clearErr
		}
		fmt.Printf("Project account cleared for %s.\n", root)
		return nil
	case "current":
		if len(o.Names) != 0 {
			return errors.New("usage: xswap project current [--path DIR]")
		}
		name, accountErr := a.accountForDirectory(directory)
		if accountErr != nil {
			return accountErr
		}
		fmt.Println(name)
		return nil
	default:
		return errors.New("usage: xswap project use NAME | switch NAME | clear | current")
	}
}

func handoffAccountLabel(a *App, name string) string {
	label := a.displayName(name)
	if label == name {
		return name
	}
	return fmt.Sprintf("%s (%s)", label, name)
}

func printProjectHandoffResult(a *App, result projectHandoffResult) {
	fmt.Printf("Account handoff: %s → %s.\n", handoffAccountLabel(a, result.Source), handoffAccountLabel(a, result.Target))
	if result.Global {
		fmt.Println("Global selection updated for unpinned directories.")
	} else {
		fmt.Printf("Project %s now uses %s.\n", result.Project, handoffAccountLabel(a, result.Target))
	}
	fmt.Printf("Conversations: %d copied, %d already present.\n", result.Copied, result.Already)
	if result.Restarted > 0 {
		fmt.Printf("Managed Codex sessions resumed automatically: %d.\n", result.Restarted)
	}
	if result.NotRestarted > 0 {
		fmt.Printf("Managed sessions that could not be resumed automatically: %d. Use codex resume to reopen them.\n", result.NotRestarted)
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
