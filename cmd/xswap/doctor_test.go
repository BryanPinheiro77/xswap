package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func doctorFixture(t *testing.T) *App {
	t.Helper()
	a := fixture(t)
	if err := os.MkdirAll(a.Root, 0700); err != nil {
		t.Fatal(err)
	}
	manager := filepath.Join(t.TempDir(), "xswap")
	if err := os.WriteFile(manager, []byte("fake xswap executable"), 0700); err != nil {
		t.Fatal(err)
	}
	a.Binary = manager
	cli := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(cli, []byte("fake codex executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(a.Root, "installation.json"), map[string]string{"cli": cli, "manager": manager}); err != nil {
		t.Fatal(err)
	}
	return a
}

func requireDoctorCheck(t *testing.T, report doctorReport, name string, want doctorStatus) {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			if check.Status != want {
				t.Fatalf("%s status = %s, want %s (%s)", name, check.Status, want, check.Detail)
			}
			return
		}
	}
	t.Fatalf("missing doctor check %q", name)
}

func TestDoctorHealthyInstallation(t *testing.T) {
	a := doctorFixture(t)
	ready(t, a, "work")
	if err := setupDoctorWrappers(t, a); err != nil {
		t.Fatal(err)
	}
	s := AutoConfig{Enabled: true, Threshold: 90, Interval: 60, Cooldown: 300, Margin: 5}
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), Settings{Auto: s}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(a.Root, "auto-daemon.json"), map[string]any{"pid": os.Getpid(), "heartbeat": time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	report := a.doctorReport()
	if report.ExitCode != 0 {
		t.Fatalf("healthy installation exit code = %d, checks: %+v", report.ExitCode, report.Checks)
	}
	for _, name := range []string{"version", "platform", "Codex CLI", "wrappers", "CODEX_HOME", "manager directory", "profiles", "configuration", "auto-switch monitor"} {
		requireDoctorCheck(t, report, name, doctorPass)
	}
}

func TestDoctorMissingCLIProvidesRemediation(t *testing.T) {
	a := doctorFixture(t)
	metadata := map[string]string{"cli": filepath.Join(t.TempDir(), "missing-codex"), "manager": a.Binary}
	if err := writeJSON(filepath.Join(a.Root, "installation.json"), metadata); err != nil {
		t.Fatal(err)
	}
	report := a.doctorReport()
	if report.ExitCode != 2 {
		t.Fatalf("missing CLI exit code = %d, want 2", report.ExitCode)
	}
	requireDoctorCheck(t, report, "Codex CLI", doctorFail)
	for _, check := range report.Checks {
		if check.Name == "Codex CLI" && !strings.Contains(check.Fix, "xswap install") {
			t.Fatalf("missing CLI remediation: %q", check.Fix)
		}
	}
}

func TestDoctorBrokenWrappersAndPendingProfilesAreWarnings(t *testing.T) {
	a := doctorFixture(t)
	if err := setupDoctorWrappers(t, a); err != nil {
		t.Fatal(err)
	}
	if _, err := a.create("pending"); err != nil {
		t.Fatal(err)
	}
	if err := breakDoctorWrapper(t, a); err != nil {
		t.Fatal(err)
	}
	report := a.doctorReport()
	if report.ExitCode != 1 {
		t.Fatalf("warning exit code = %d, want 1", report.ExitCode)
	}
	requireDoctorCheck(t, report, "wrappers", doctorWarning)
	requireDoctorCheck(t, report, "profiles", doctorWarning)
}

func TestDoctorInvalidSettingsAreFailureAndDoNotEchoContents(t *testing.T) {
	a := doctorFixture(t)
	secret := "do-not-print-this-test-value"
	if err := os.WriteFile(filepath.Join(a.Root, "settings.json"), []byte(`{"private":"`+secret+`","auto":{"threshold":999}}`), 0600); err != nil {
		t.Fatal(err)
	}
	report := a.doctorReport()
	if report.ExitCode != 2 {
		t.Fatalf("invalid settings exit code = %d, want 2", report.ExitCode)
	}
	requireDoctorCheck(t, report, "configuration", doctorFail)
	for _, check := range report.Checks {
		if strings.Contains(check.Detail, secret) || strings.Contains(check.Fix, secret) {
			t.Fatal("doctor output included settings contents")
		}
	}
	var output bytes.Buffer
	a.doctor(&output)
	if strings.Contains(output.String(), secret) || strings.Contains(output.String(), "auth.json") || strings.Contains(output.String(), "quota payload") {
		t.Fatal("doctor printed private account data")
	}
}

func TestDoctorReportsExplicitCODEXHomePrecedence(t *testing.T) {
	a := doctorFixture(t)
	home := filepath.Join(t.TempDir(), "explicit-profile")
	t.Setenv("CODEX_HOME", home)
	report := a.doctorReport()
	requireDoctorCheck(t, report, "CODEX_HOME", doctorPass)
	for _, check := range report.Checks {
		if check.Name == "CODEX_HOME" && !strings.Contains(check.Detail, "takes precedence") {
			t.Fatalf("CODEX_HOME precedence not explained: %q", check.Detail)
		}
	}
}

func TestDoctorMonitorMetadataRequiresFreshHeartbeat(t *testing.T) {
	a := doctorFixture(t)
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), Settings{Auto: AutoConfig{Enabled: true, Threshold: 90, Interval: 30, Cooldown: 300, Margin: 5}}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(a.Root, "auto-daemon.json"), map[string]any{"pid": os.Getpid(), "heartbeat": time.Now().Add(-time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	report := a.doctorReport()
	requireDoctorCheck(t, report, "auto-switch monitor", doctorWarning)
}

func TestDoctorReportsOverlyBroadManagerDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not describe Windows ACLs")
	}
	a := doctorFixture(t)
	if err := os.Chmod(a.Root, 0755); err != nil {
		t.Fatal(err)
	}
	report := a.doctorReport()
	requireDoctorCheck(t, report, "manager directory", doctorWarning)
	for _, check := range report.Checks {
		if check.Name == "manager directory" && !strings.Contains(check.Fix, "chmod 700") {
			t.Fatalf("manager permission remediation: %q", check.Fix)
		}
	}
}

func TestDoctorDoesNotMutateSelectionOrSettings(t *testing.T) {
	a := doctorFixture(t)
	if err := a.selectAccount("default"); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), Settings{Auto: AutoConfig{Threshold: 90, Interval: 60, Cooldown: 300, Margin: 5}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(a.Root, "active"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = a.settings()
	settingsBefore, err := os.ReadFile(filepath.Join(a.Root, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = a.doctorReport()
	after, err := os.ReadFile(filepath.Join(a.Root, "active"))
	if err != nil {
		t.Fatal(err)
	}
	settingsAfter, err := os.ReadFile(filepath.Join(a.Root, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || string(settingsBefore) != string(settingsAfter) {
		t.Fatal("doctor modified account selection or settings")
	}
}

func TestDoctorCommandExitCodes(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		a := doctorFixture(t)
		if err := setupDoctorWrappers(t, a); err != nil {
			t.Fatal(err)
		}
		if err := a.run([]string{"doctor"}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("warnings", func(t *testing.T) {
		a := doctorFixture(t)
		err := a.run([]string{"doctor"})
		var coded interface{ ExitCode() int }
		if !errors.As(err, &coded) || coded.ExitCode() != 1 {
			t.Fatalf("warning exit = %v, want 1", err)
		}
	})
	t.Run("failure", func(t *testing.T) {
		a := doctorFixture(t)
		if err := writeJSON(filepath.Join(a.Root, "settings.json"), Settings{Auto: AutoConfig{Threshold: 999, Interval: 60}}); err != nil {
			t.Fatal(err)
		}
		err := a.run([]string{"doctor"})
		var coded interface{ ExitCode() int }
		if !errors.As(err, &coded) || coded.ExitCode() != 2 {
			t.Fatalf("failure exit = %v, want 2", err)
		}
	})
}
