package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeServiceManager struct {
	loaded   bool
	failWith string
	commands []string
}

func (f *fakeServiceManager) run(_ context.Context, name string, args ...string) (string, error) {
	command := name + " " + strings.Join(args, " ")
	f.commands = append(f.commands, command)
	if f.failWith != "" && strings.Contains(command, f.failWith) {
		return "", errors.New("simulated service manager failure")
	}
	if name == "launchctl" {
		switch args[0] {
		case "print":
			if !f.loaded {
				return "", errors.New("service not loaded")
			}
			return "loaded", nil
		case "bootstrap":
			f.loaded = true
		case "bootout":
			f.loaded = false
		}
	}
	if name == "systemctl" {
		switch args[1] {
		case "is-enabled":
			if !f.loaded {
				return "disabled", errors.New("service disabled")
			}
			return "enabled", nil
		case "enable":
			f.loaded = true
		case "disable":
			f.loaded = false
		}
	}
	if name == "journalctl" {
		return "test journal entry\n", nil
	}
	return "", nil
}

func serviceFixture(t *testing.T) (*App, *fakeServiceManager) {
	t.Helper()
	a := fixture(t)
	a.Binary = filepath.Join(t.TempDir(), "xswap")
	if err := os.WriteFile(a.Binary, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	manager := &fakeServiceManager{}
	a.ServiceRunner = manager.run
	return a, manager
}

func TestServiceUsesStableExecutableLinkWhenAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("user services are not supported on Windows")
	}
	a, _ := serviceFixture(t)
	binDir := t.TempDir()
	link := filepath.Join(binDir, "xswap")
	if err := os.Symlink(a.Binary, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	s, err := a.serviceSpec()
	if err != nil {
		t.Fatal(err)
	}
	if s.Binary != link {
		t.Fatalf("service executable = %q, want stable link %q", s.Binary, link)
	}
	other := filepath.Join(t.TempDir(), "other-xswap")
	if err := os.WriteFile(other, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if got := stableServiceExecutable(a.Binary); got != a.Binary {
		t.Fatalf("used unrelated executable %q", got)
	}
}

func TestServiceDefinitionsAreScopedAndRestartOnlyAfterFailure(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with spaces")
	root := filepath.Join(home, "state & <test> $%")
	binary := filepath.Join(home, "bin", "xswap $%")
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			s, err := newServiceSpec(platform, home, root, binary)
			if err != nil {
				t.Fatal(err)
			}
			data, err := s.definition()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte(s.marker())) || !bytes.Contains(data, []byte("__daemon")) {
				t.Fatal("service is missing its ownership marker or monitor command")
			}
			if platform == "darwin" {
				decoder := xml.NewDecoder(bytes.NewReader(data))
				for {
					_, err := decoder.Token()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						t.Fatal("invalid launchd plist:", err)
					}
				}
				for _, text := range []string{"<key>RunAtLoad</key><true/>", "<key>SuccessfulExit</key><false/>", "<key>ThrottleInterval</key><integer>30</integer>", "&amp;", "&lt;test&gt;"} {
					if !bytes.Contains(data, []byte(text)) {
						t.Fatal("missing launchd setting", text)
					}
				}
			} else {
				for _, text := range []string{"Restart=on-failure", "RestartSec=30s", "StartLimitBurst=5", `xswap $$%%`, `CODEX_SWAP_HOME=`, `state & <test> $%%`} {
					if !bytes.Contains(data, []byte(text)) {
						t.Fatal("missing systemd setting", text)
					}
				}
			}
		})
	}
}

func TestServiceInstallAndUninstallUseOnlyTemporaryUserFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("user services are not supported on Windows")
	}
	a, manager := serviceFixture(t)
	if err := a.installService(); err != nil {
		t.Fatal(err)
	}
	s, _ := a.serviceSpec()
	if data, installed, err := s.readOwned(); err != nil || !installed || len(data) == 0 {
		t.Fatal("service file was not installed", installed, err)
	}
	if !manager.loaded {
		t.Fatal("user service was not registered")
	}
	if err := a.installService(); err != nil {
		t.Fatal("idempotent install failed:", err)
	}
	if err := a.uninstallService(); err != nil {
		t.Fatal(err)
	}
	if exists(s.Path) || manager.loaded {
		t.Fatal("service remained after uninstall")
	}
	if err := a.uninstallService(); err != nil {
		t.Fatal("idempotent uninstall failed:", err)
	}
	settings, err := a.settings()
	if err != nil || settings.Auto.Enabled {
		t.Fatal("service operations changed auto-switch settings", err)
	}
	if len(manager.commands) == 0 {
		t.Fatal("service manager was not called")
	}
}

func TestServiceRefusesForeignAndSymlinkFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("user services are not supported on Windows")
	}
	a, manager := serviceFixture(t)
	s, _ := a.serviceSpec()
	if err := secureServiceDirectory(filepath.Dir(a.DefaultHome), filepath.Dir(s.Path)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, []byte("unrelated user service"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.installService(); err == nil {
		t.Fatal("overwrote a foreign service")
	}
	if err := a.uninstallService(); err == nil {
		t.Fatal("removed a foreign service")
	}
	if len(manager.commands) != 0 {
		t.Fatal("changed service manager state for foreign service", manager.commands)
	}
	if err := os.Remove(s.Path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, s.Path); err != nil {
		t.Fatal(err)
	}
	if err := a.installService(); err == nil {
		t.Fatal("accepted a symlink service file")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "untouched" {
		t.Fatal("modified symlink target", err)
	}
}

func TestServiceRegistrationFailureRemovesNewFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("user services are not supported on Windows")
	}
	a, manager := serviceFixture(t)
	if runtime.GOOS == "darwin" {
		manager.failWith = "bootstrap"
	} else {
		manager.failWith = " enable "
	}
	if err := a.installService(); err == nil {
		t.Fatal("ignored service manager failure")
	}
	s, _ := a.serviceSpec()
	if exists(s.Path) {
		t.Fatal("failed installation left a user service behind")
	}
}

func TestServiceInstallStopsExistingDetachedMonitor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("user services are not supported on Windows")
	}
	a, manager := serviceFixture(t)
	helperCLI(t, a)
	settings, _ := a.settings()
	settings.Auto.Enabled = true
	settings.Auto.Interval = 10
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), settings); err != nil {
		t.Fatal(err)
	}
	binary, _ := os.Executable()
	child := exec.Command(binary, "-test.run=TestDaemonHelperProcess")
	child.Env = envWith(envWith(os.Environ(), "CODEX_SWAP_HOME", a.Root), "XSWAP_TEST_DAEMON", "1")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for !a.daemonRunning() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !a.daemonRunning() {
		t.Fatal("test monitor did not start")
	}
	if err := a.installService(); err != nil {
		t.Fatal(err)
	}
	if a.daemonRunning() {
		t.Fatal("detached monitor still owns singleton lock")
	}
	settings, _ = a.settings()
	if !settings.Auto.Enabled || !manager.loaded {
		t.Fatal("service migration did not restore auto-switch or register supervisor")
	}
}

func TestServiceLogsAreBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("user services are not supported on Windows")
	}
	a, _ := serviceFixture(t)
	if err := a.installService(); err != nil {
		t.Fatal(err)
	}
	s, _ := a.serviceSpec()
	if s.Platform == "darwin" {
		lines := make([]string, 105)
		for i := range lines {
			lines[i] = "line " + strconv.Itoa(i+1)
		}
		if err := os.WriteFile(s.LogPath, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := a.serviceLogs(&output); err != nil {
		t.Fatal(err)
	}
	if s.Platform == "darwin" {
		if strings.Contains(output.String(), "line 5\n") || !strings.Contains(output.String(), "line 6\n") || !strings.Contains(output.String(), "line 105\n") {
			t.Fatal("log tail did not keep the last 100 lines")
		}
	} else if output.String() != "test journal entry\n" {
		t.Fatal("journal output was not returned", output.String())
	}
}
