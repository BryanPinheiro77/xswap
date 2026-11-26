package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func testConfigExport() configExport {
	return configExport{Format: "xswap-config", Version: 1, Accounts: []exportAccount{
		{Name: "default", Label: "Source default", Disabled: true},
		{Name: "work", Label: "Work", Disabled: true},
		{Name: "personal"},
	}, Auto: &AutoConfig{Enabled: true, Threshold: 81, Interval: 45, Cooldown: 120, Margin: 7, Threshold5h: pointer(75)}}
}

func TestConfigExportRoundTripAndIsolation(t *testing.T) {
	source := fixture(t)
	ready(t, source, "work")
	ready(t, source, "personal")
	settings, _ := source.settings()
	settings.Disabled = []string{"default", "work", "removed"}
	settings.DisplayNames = map[string]string{"default": "Source default", "work": "Work", "removed": "omit this"}
	settings.Auto = *testConfigExport().Auto
	if err := writeJSON(filepath.Join(source.Root, "settings.json"), settings); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(source.DefaultHome, "auth.json"), filepath.Join(source.DefaultHome, "config.toml"), filepath.Join(source.Root, "profiles", "work", "auth.json"), filepath.Join(source.Root, "profiles", "work", "conversation.jsonl")} {
		if err := os.WriteFile(path, []byte("fake-private-content-never-export"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "config.json")
	var out bytes.Buffer
	if err := source.transferCommand("export", []string{path}, &out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"fake-private", source.Root, source.DefaultHome, "auth.json", "conversation", "omit this", "removed"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("export includes disallowed data: %q", forbidden)
		}
	}
	if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("export permissions", info, err)
	}
	document, err := readConfigExport(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	destination := fixture(t)
	ready(t, destination, "local")
	if err := destination.selectAccount("local"); err != nil {
		t.Fatal(err)
	}
	local, _ := destination.settings()
	local.DisplayNames["default"] = "Local default"
	local.Disabled = []string{"local"}
	if err := writeJSON(filepath.Join(destination.Root, "settings.json"), local); err != nil {
		t.Fatal(err)
	}
	lastSwitch, _ := os.ReadFile(filepath.Join(destination.Root, "last-switch"))
	defaultAuth := filepath.Join(destination.DefaultHome, "auth.json")
	if err := os.WriteFile(defaultAuth, []byte("fake-original-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := destination.importConfig(document, importOptions{ApplyAuto: true})
	if err != nil || !reflect.DeepEqual(result.Skipped, []string{"default"}) || len(result.Created) != 2 {
		t.Fatal(result, err)
	}
	got, err := destination.settings()
	if err != nil || got.Auto.Enabled || got.Auto.Threshold != 81 || got.Auto.Threshold5h == nil || *got.Auto.Threshold5h != 75 || disabled(got, "default") || !disabled(got, "work") || !disabled(got, "local") || got.DisplayNames["default"] != "Local default" || got.DisplayNames["work"] != "Work" {
		t.Fatal("settings not safely merged", got, err)
	}
	currentLastSwitch, _ := os.ReadFile(filepath.Join(destination.Root, "last-switch"))
	credential, _ := os.ReadFile(defaultAuth)
	if destination.selected() != "local" || !bytes.Equal(lastSwitch, currentLastSwitch) || string(credential) != "fake-original-credential" {
		t.Fatal("import changed selection, cooldown or original credentials")
	}
	for _, name := range result.Created {
		entries, err := os.ReadDir(filepath.Join(destination.Root, "profiles", name))
		if err != nil || len(entries) != 1 || entries[0].Name() != profileMarker || destination.localAuthentication(name) != authPending {
			t.Fatal("import copied more than registration metadata", name, entries, err)
		}
		if err := destination.selectAccount(name); err == nil {
			t.Fatal("imported account became selectable without login")
		}
	}
}

func TestConfigImportConflictsDryRunAndAutoPolicy(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	if err := a.setDisplayName("work", "Keep me"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(a.Root, "settings.json"))
	document := testConfigExport()
	if _, err := a.importConfig(document, importOptions{}); err == nil {
		t.Fatal("import overwrote existing profile")
	}
	if exists(filepath.Join(a.Root, "profiles", "personal")) {
		t.Fatal("failed preflight imported a profile")
	}
	result, err := a.importConfig(document, importOptions{SkipExisting: true, DryRun: true, ApplyAuto: true})
	if err != nil || !reflect.DeepEqual(result.Created, []string{"personal"}) || !reflect.DeepEqual(result.Skipped, []string{"default", "work"}) {
		t.Fatal(result, err)
	}
	after, _ := os.ReadFile(filepath.Join(a.Root, "settings.json"))
	if !bytes.Equal(before, after) || exists(filepath.Join(a.Root, "profiles", "personal")) {
		t.Fatal("dry-run mutated state")
	}
	if _, err := a.importConfig(document, importOptions{SkipExisting: true}); err != nil {
		t.Fatal(err)
	}
	settings, _ := a.settings()
	if settings.Auto.Threshold != 90 || settings.Auto.Enabled || settings.DisplayNames["work"] != "Keep me" || disabled(settings, "work") {
		t.Fatal("default import changed local policy or skipped metadata", settings)
	}
	settings.Auto.Enabled = true
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), settings); err != nil {
		t.Fatal(err)
	}
	document.Auto.Enabled = false
	if _, err := a.importConfig(document, importOptions{SkipExisting: true, ApplyAuto: true}); err != nil {
		t.Fatal(err)
	}
	settings, _ = a.settings()
	if !settings.Auto.Enabled || settings.Auto.Threshold != 81 {
		t.Fatal("apply-auto changed enabled state", settings.Auto)
	}
	missing := &App{Root: filepath.Join(t.TempDir(), "absent"), DefaultHome: t.TempDir()}
	if _, err := missing.importConfig(document, importOptions{DryRun: true}); err != nil || exists(missing.Root) {
		t.Fatal("dry-run created a manager directory", err)
	}
}

func TestConfigImportRejectsMalformedInput(t *testing.T) {
	data, _ := json.Marshal(testConfigExport())
	valid := string(data)
	for _, input := range []string{
		"", "null", "[]", valid + valid,
		strings.Replace(valid, `"version":1`, `"version":2`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"Version":1`, 1),
		strings.Replace(valid, `"work"`, `"../escape"`, 1),
		strings.Replace(valid, `"work"`, `"C:\\escape"`, 1),
		strings.Replace(valid, `"personal"`, `"WORK"`, 1),
		strings.Replace(valid, `"default"`, `"DEFAULT"`, 1),
		strings.Replace(valid, `"threshold":81`, `"threshold":0`, 1),
		strings.Replace(valid, `"accounts":[`, `"accounts":null,"ignored":[`, 1),
		strings.Replace(valid, `"disabled":true`, `"disabled":null`, 1),
		strings.Replace(valid, `"label":"Work"`, `"label":"\u001b[31msecret"`, 1),
		strings.Replace(valid, `"version":1`, `"secret":"fake-private-content","version":1`, 1),
		strings.Repeat(" ", maxConfigExport+1),
	} {
		_, err := readConfigExport(strings.NewReader(input))
		if err == nil || strings.Contains(err.Error(), "fake-private-content") {
			t.Fatalf("accepted malformed input or exposed content: %v", err)
		}
	}
	if _, err := readConfigExport(strings.NewReader(valid)); err != nil {
		t.Fatal("rejected valid input", err)
	}
}

func TestConfigImportPreservesUnknownLocalSettingsAndRejectsLeftovers(t *testing.T) {
	a := fixture(t)
	settingsPath := filepath.Join(a.Root, "settings.json")
	data := []byte(`{"futurePreference":"keep-me"}`)
	if err := atomicWrite(settingsPath, data); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importConfig(testConfigExport(), importOptions{}); err == nil {
		t.Fatal("import would discard unknown local preferences")
	}
	got, _ := os.ReadFile(settingsPath)
	if !bytes.Equal(data, got) || exists(filepath.Join(a.Root, "profiles")) {
		t.Fatal("unknown local settings changed")
	}
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(a.Root, "profiles", "work"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importConfig(testConfigExport(), importOptions{SkipExisting: true}); err == nil {
		t.Fatal("import reused an unregistered leftover directory")
	}
}

func TestConfigImportRollbackPreservesProfilesAndHealth(t *testing.T) {
	a := fixture(t)
	if err := atomicWrite(a.healthPath("work"), []byte("old-safe-health")); err != nil {
		t.Fatal(err)
	}
	// The second profile fails after the first is published. No existing file or
	// observations may be lost, and no partial account may remain registered.
	if err := os.Mkdir(a.healthPath("personal"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importConfig(testConfigExport(), importOptions{}); err == nil {
		t.Fatal("accepted invalid health destination")
	}
	oldHealth, _ := os.ReadFile(a.healthPath("work"))
	if string(oldHealth) != "old-safe-health" || exists(filepath.Join(a.Root, "settings.json")) || !reflect.DeepEqual(a.names(), []string{"default"}) {
		t.Fatal("rollback did not preserve state")
	}
	stages, _ := filepath.Glob(filepath.Join(a.Root, ".import-*"))
	if len(stages) != 0 {
		t.Fatal("import staging leaked", stages)
	}
	if err := os.Remove(a.healthPath("personal")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importConfig(testConfigExport(), importOptions{}); err != nil || exists(a.healthPath("work")) {
		t.Fatal("reused account inherited stale health", err)
	}
}

func TestConfigTransferFilesAndUnsafeStorage(t *testing.T) {
	a := fixture(t)
	for _, args := range [][]string{{"export"}, {"import"}, {"auto", "on", "--apply-auto"}, {"list", "--skip-existing"}} {
		if err := a.run(args); err == nil {
			t.Fatal("accepted invalid CLI route", args)
		}
	}
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "export.json")
	if err := a.transferCommand("export", []string{path}, &out); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := a.transferCommand("export", []string{path}, &out); err == nil {
		t.Fatal("export overwrote an existing file")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing export changed")
	}
	for _, args := range [][]string{nil, {path, "extra"}, {path, "--yes"}, {path, "--threshold", "90"}} {
		if err := a.transferCommand("import", args, &out); err == nil {
			t.Fatal("accepted invalid import arguments", args)
		}
	}
	for _, destination := range []string{filepath.Join(a.Root, "other.json"), filepath.Join(a.DefaultHome, "auth.json")} {
		if err := a.transferCommand("export", []string{destination}, &out); err == nil {
			t.Fatal("export wrote into account storage")
		}
	}
	if err := a.transferCommand("import", []string{path, "--dry-run"}, &out); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlinks may require Windows developer mode")
	}
	linkedFile := filepath.Join(t.TempDir(), "link.json")
	if err := os.Symlink(path, linkedFile); err != nil {
		t.Fatal(err)
	}
	if err := a.transferCommand("import", []string{linkedFile}, &out); err == nil {
		t.Fatal("followed a symlink import file")
	}
	if err := os.MkdirAll(filepath.Join(a.Root, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := atomicWrite(filepath.Join(outside, profileMarker), []byte("1\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(a.Root, "profiles", "work")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importConfig(testConfigExport(), importOptions{SkipExisting: true}); err == nil {
		t.Fatal("reused a symlink profile")
	}
	b := fixture(t)
	if err := os.MkdirAll(b.Root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(b.Root, "profiles")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.importConfig(testConfigExport(), importOptions{}); err == nil {
		t.Fatal("followed symlink profile parent")
	}
	if err := os.Symlink(path, filepath.Join(b.Root, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.exportConfig(); err == nil {
		t.Fatal("export followed unsafe storage")
	}
}
