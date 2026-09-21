package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCLIParsingAndValidation(t *testing.T) {
	o, err := parse([]string{"work", "--yes", "--interval", "30", "--repo", "owner/xswap"})
	if err != nil || !reflect.DeepEqual(o.Names, []string{"work"}) || !o.Flags["yes"] || o.Values["repo"] != "owner/xswap" {
		t.Fatal(o, err)
	}
	if interval, err := o.integer("interval", 60); err != nil || interval != 30 {
		t.Fatal(interval, err)
	}
	if value, err := o.integer("threshold", 90); err != nil || value != 90 {
		t.Fatal(value, err)
	}
	for _, args := range [][]string{{"--unknown"}, {"--interval"}, {"--repo"}} {
		if _, err := parse(args); err == nil {
			t.Fatal("accepted", args)
		}
	}
	a := fixture(t)
	for _, args := range [][]string{{"unknown"}, {"run"}, {"login"}, {"switch"}, {"disable"}, {"enable"}, {"remove"}, {"list", "one", "two"}, {"watch", "--interval", "bad"}, {"watch", "--interval", "1"}, {"watch", "missing"}, {"limits", "work", "--all"}, {"remove", "default"}, {"add", "default"}, {"switch", "missing"}, {"auto", "unknown"}, {"auto", "status", "--threshold", "0"}, {"auto", "status", "--threshold", "101"}, {"auto", "status", "--threshold", "bad"}, {"auto", "status", "--interval", "1"}} {
		if err := a.run(args); err == nil {
			t.Fatal("accepted invalid command", args)
		}
	}
	for _, args := range [][]string{{"--help"}, {"version"}, {"list"}, {"current"}, {"auto", "off"}, {"auto", "status", "--threshold", "80", "--interval", "30"}} {
		if err := a.run(args); err != nil {
			t.Fatal(args, err)
		}
	}
	s, err := a.settings()
	if err != nil || s.Auto.Enabled || s.Auto.Threshold != 80 || s.Auto.Interval != 30 {
		t.Fatal(s, err)
	}
}

func TestCLIRenameDisplayName(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	if err := a.run([]string{"rename", "work", "--label", "Work"}); err != nil {
		t.Fatal(err)
	}
	s, err := a.settings()
	if err != nil || s.DisplayNames["work"] != "Work" {
		t.Fatal(s, err)
	}
	if err := a.run([]string{"rename", "work", "--label", ""}); err != nil {
		t.Fatal(err)
	}
	s, err = a.settings()
	if err != nil || len(s.DisplayNames) != 0 {
		t.Fatal(s, err)
	}
}

func TestNewAppReadsHomebrewWrapperMetadata(t *testing.T) {
	stable := filepath.Join(t.TempDir(), "opt", "xswap", "bin", "xswap")
	t.Setenv("XSWAP_PACKAGE_MANAGER", "homebrew")
	t.Setenv("XSWAP_EXECUTABLE", stable)
	a := newApp()
	if a.PackageManager != "homebrew" || a.Binary != stable {
		t.Fatal("Homebrew wrapper metadata ignored", a.PackageManager, a.Binary)
	}

	t.Setenv("XSWAP_PACKAGE_MANAGER", "unknown")
	t.Setenv("XSWAP_EXECUTABLE", stable)
	a = newApp()
	if a.PackageManager != "" || a.Binary == stable {
		t.Fatal("unknown package manager metadata accepted", a.PackageManager, a.Binary)
	}
}

func TestBrowserAndDeviceLoginUseOnlyNamedTemporaryProfile(t *testing.T) {
	for _, device := range []bool{false, true} {
		t.Run(map[bool]string{false: "browser", true: "device"}[device], func(t *testing.T) {
			a := fixture(t)
			if _, err := a.create("work"); err != nil {
				t.Fatal(err)
			}
			cli := filepath.Join(t.TempDir(), "fake-codex")
			script := "#!/bin/sh\n" + `printf '%s\n' "$@" > "$CODEX_HOME/login-args"` + "\n" + `printf '%s' 'fake-test-credential' > "$CODEX_HOME/auth.json"` + "\n"
			if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			if err := writeJSON(filepath.Join(a.Root, "installation.json"), map[string]string{"cli": cli}); err != nil {
				t.Fatal(err)
			}
			if err := a.login("work", device); err != nil {
				t.Fatal(err)
			}
			home, _ := a.profile("work")
			args, err := os.ReadFile(filepath.Join(home, "login-args"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(args), "login") || strings.Contains(string(args), "--device-auth") != device {
				t.Fatal(string(args))
			}
			info, err := os.Stat(filepath.Join(home, "auth.json"))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("credential permissions", err)
			}
			if exists(filepath.Join(a.DefaultHome, "auth.json")) || a.selected() != "default" {
				t.Fatal("login changed default account")
			}
			if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := a.login("work", device); err == nil {
				t.Fatal("ignored failed login")
			}
		})
	}
}

func TestManagementAndQuotaCommandsInIsolatedProfiles(t *testing.T) {
	a := fixture(t)
	helperCLI(t, a)
	ready(t, a, "work")
	t.Setenv("TERM", "dumb")
	for _, args := range [][]string{{"switch", "work"}, {"disable", "work"}, {"list"}, {"enable", "work"}, {"limits", "work"}, {"watch", "work", "--once"}, {"auto", "--once", "--dry-run", "--threshold", "80", "--interval", "30"}} {
		if err := a.run(args); err != nil {
			t.Fatal(args, err)
		}
	}
	if a.selected() != "work" {
		t.Fatal("dry-run changed selected account")
	}
	s, err := a.settings()
	if err != nil || s.Auto.Enabled || s.Auto.Threshold != 90 || s.Auto.Interval != 60 {
		t.Fatal("dry-run persisted overrides", s, err)
	}
	if err := a.run([]string{"switch", "default"}); err != nil {
		t.Fatal(err)
	}
	if err := a.run([]string{"remove", "work", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.names(), []string{"default"}) {
		t.Fatal("CLI did not remove profile")
	}
	if err := a.run([]string{"limits", "missing"}); err == nil {
		t.Fatal("quota command ignored unregistered account")
	}
}
