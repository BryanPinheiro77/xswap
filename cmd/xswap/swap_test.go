package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixture(t *testing.T) *App {
	t.Helper()
	directory := t.TempDir()
	a := &App{Root: filepath.Join(directory, "state"), DefaultHome: filepath.Join(directory, "default")}
	os.MkdirAll(a.DefaultHome, 0700)
	return a
}
func ready(t *testing.T, a *App, name string) {
	t.Helper()
	if _, err := a.create(name); err != nil {
		t.Fatal(err)
	}
	home, _ := a.profile(name)
	if err := atomicWrite(filepath.Join(home, "auth.json"), []byte("{}")); err != nil {
		t.Fatal(err)
	}
}
func pointer[T any](v T) *T { return &v }
func quota(used, weekly float64, now int64) Record {
	return Record{Account: Account{Type: "chatgpt"}, Updated: now, Limits: Limits{Main: &Bucket{ID: "codex", Primary: &Window{Used: pointer(used), Minutes: pointer(300), Resets: pointer(now + 3600)}, Secondary: &Window{Used: pointer(weekly), Minutes: pointer(10080), Resets: pointer(now + 86400)}}}}
}

func TestIsolationAndMigration(t *testing.T) {
	a := fixture(t)
	atomicWrite(filepath.Join(a.DefaultHome, "auth.json"), []byte("private token"))
	atomicWrite(filepath.Join(a.DefaultHome, "config.toml"), []byte("model = 'example'"))
	os.Mkdir(filepath.Join(a.DefaultHome, "skills"), 0700)
	os.Mkdir(filepath.Join(a.DefaultHome, "sessions"), 0700)
	name, err := a.createNumbered()
	if err != nil || name != "account-1" {
		t.Fatal(name, err)
	}
	home, _ := a.profile(name)
	if exists(filepath.Join(home, "auth.json")) || exists(filepath.Join(home, "sessions")) {
		t.Fatal("copied auth or sessions")
	}
	info, _ := os.Stat(filepath.Join(home, "config.toml"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("config permissions")
	}
	link, err := os.Readlink(filepath.Join(home, "skills"))
	if err != nil || link != filepath.Join(a.DefaultHome, "skills") {
		t.Fatal("skills link", err)
	}
	name, err = a.createNumbered()
	if err != nil || name != "account-2" {
		t.Fatal(name, err)
	}
	if a.selected() != "default" {
		t.Fatal("changed current account")
	}
	for _, name := range []string{"../escape", "..", "/tmp/path", "", "default"} {
		if _, err = a.create(name); err == nil {
			t.Fatal("accepted invalid name", name)
		}
	}
}
func TestDisableAndRemovalPreserveData(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	if err := a.setEnabled("work", false); err != nil {
		t.Fatal(err)
	}
	s, _ := a.settings()
	if !disabled(s, "work") {
		t.Fatal("not disabled")
	}
	if err := a.selectAccount("work"); err != nil {
		t.Fatal("manual selection of disabled account", err)
	}
	if _, err := a.remove("work"); err == nil {
		t.Fatal("removed active account")
	}
	if _, err := a.remove("default"); err == nil {
		t.Fatal("removed default")
	}
	a.selectAccount("default")
	archive, err := a.remove("work")
	if err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(archive, "auth.json")) {
		t.Fatal("lost credentials")
	}
	if !reflect.DeepEqual(a.names(), []string{"default"}) {
		t.Fatal(a.names())
	}
	if !exists(a.DefaultHome) {
		t.Fatal("lost original home")
	}
}
func TestCommandsHonorExplicitHomeOnlyForCodex(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	writeJSON(filepath.Join(a.Root, "installation.json"), map[string]string{"cli": "/usr/bin/true"})
	t.Setenv("CODEX_HOME", "/explicit")
	_, args, env, err := a.command("work", []string{"exec", "prompt with spaces"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"exec", "prompt with spaces"}) || !containsEnv(env, "CODEX_HOME=/explicit") {
		t.Fatal(args, env)
	}
	_, args, env, err = a.command("work", []string{"--version"}, false)
	home, _ := a.profile("work")
	if err != nil || !containsEnv(env, "CODEX_HOME="+home) || args[1] != `cli_auth_credentials_store="file"` {
		t.Fatal(args, err)
	}
}
func containsEnv(env []string, item string) bool {
	for _, entry := range env {
		if entry == item {
			return true
		}
	}
	return false
}
func TestAutoSwitchDecisions(t *testing.T) {
	now := time.Now().Unix()
	s := Settings{Auto: AutoConfig{true, 90, 60, 300, 5}}
	records := map[string]Record{"default": quota(95, 80, now), "work": quota(20, 40, now), "other": quota(70, 30, now)}
	target, _ := decide(records, "default", s, now, 0)
	if target != "work" {
		t.Fatal(target)
	}
	cases := []struct {
		name   string
		mutate func(map[string]Record, *Settings)
		last   int64
	}{
		{"cooldown", func(_ map[string]Record, _ *Settings) {}, now},
		{"disabled alternatives", func(_ map[string]Record, s *Settings) { s.Disabled = []string{"work", "other"} }, 0},
		{"disabled active", func(_ map[string]Record, s *Settings) { s.Disabled = []string{"default"} }, 0},
		{"active below threshold", func(r map[string]Record, _ *Settings) { r["default"] = quota(20, 40, now) }, 0},
		{"active read error", func(r map[string]Record, _ *Settings) { v := r["default"]; v.Error = "offline"; r["default"] = v }, 0},
		{"stale alternatives", func(r map[string]Record, _ *Settings) {
			for _, name := range []string{"work", "other"} {
				v := r[name]
				v.Updated = now - 121
				r[name] = v
			}
		}, 0},
		{"api key alternatives", func(r map[string]Record, _ *Settings) {
			for _, name := range []string{"work", "other"} {
				v := r[name]
				v.Account.Type = "apiKey"
				r[name] = v
			}
		}, 0},
		{"expired active window", func(r map[string]Record, _ *Settings) {
			v := r["default"]
			v.Limits.Main.Primary.Resets = pointer(now - 1)
			r["default"] = v
		}, 0},
		{"insufficient improvement", func(r map[string]Record, _ *Settings) {
			r["default"] = quota(90, 80, now)
			r["work"] = quota(88, 40, now)
			r["other"] = quota(89, 50, now)
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := map[string]Record{"default": quota(95, 80, now), "work": quota(20, 40, now), "other": quota(70, 30, now)}
			config := s
			tc.mutate(r, &config)
			if target, _ := decide(r, "default", config, now, tc.last); target != "" {
				t.Fatal("unexpected switch", target)
			}
		})
	}
}
func TestAutoCommitDryRunAndConcurrentSelection(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	now := time.Now().Unix()
	records := map[string]Record{"default": quota(95, 80, now), "work": quota(10, 20, now)}
	s, _ := a.settings()
	s.Auto.Enabled = true
	writeJSON(filepath.Join(a.Root, "settings.json"), s)
	result, err := a.evaluate(records, "default", true, false)
	if err != nil || result.Switched || result.Target != "work" || a.selected() != "default" {
		t.Fatal(result, err)
	}
	result, err = a.evaluate(records, "default", false, false)
	if err != nil || !result.Switched || a.selected() != "work" {
		t.Fatal(result, err)
	}
	result, err = a.evaluate(records, "default", false, false)
	if err != nil || result.Switched {
		t.Fatal("overwrote concurrent selection", result, err)
	}
	a.selectAccount("default")
	s.Auto.Enabled = false
	writeJSON(filepath.Join(a.Root, "settings.json"), s)
	result, err = a.evaluate(records, "default", false, false)
	if err != nil || result.Switched {
		t.Fatal("switched after off", result, err)
	}
}
func TestPanelMenuWatchBackAndManagement(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	p := Panel{Mode: "home", Records: map[string]Record{}, Width: 120, Height: 30}
	s, _ := a.settings()
	rendered := stripANSI(p.render(a, a.names(), s, time.Now()))
	for _, label := range menuItems {
		if !strings.Contains(rendered, label) {
			t.Fatal("missing menu item", label)
		}
	}
	p.key(a, a.names(), "w")
	if p.Mode != "watch" {
		t.Fatal(p.Mode)
	}
	p.key(a, a.names(), "esc")
	if p.Mode != "home" {
		t.Fatal(p.Mode)
	}
	p.MenuCursor = 5
	p.key(a, a.names(), "enter")
	p.Cursor = 1
	p.key(a, a.names(), "enter")
	s, _ = a.settings()
	if !disabled(s, "work") {
		t.Fatal("disable UI")
	}
	p.key(a, a.names(), "enter")
	s, _ = a.settings()
	if disabled(s, "work") {
		t.Fatal("enable UI")
	}
	p.Mode = "remove"
	p.key(a, a.names(), "enter")
	if p.Mode != "confirm" {
		t.Fatal(p.Mode)
	}
	p.key(a, a.names(), "esc")
	if !reflect.DeepEqual(a.names(), []string{"default", "work"}) {
		t.Fatal("cancel removed account")
	}
	p.Mode = "remove"
	p.Cursor = 1
	p.key(a, a.names(), "enter")
	p.key(a, a.names(), "y")
	if !reflect.DeepEqual(a.names(), []string{"default"}) {
		t.Fatal("remove UI", a.names())
	}
}
func TestThinBarsAndQuotaBuckets(t *testing.T) {
	now := time.Now()
	record := quota(81, 87, now.Unix())
	record.Limits.Buckets = map[string]Bucket{"reserve": {Primary: &Window{Used: pointer(0.0), Minutes: pointer(10080), Resets: pointer(now.Unix() + 86400)}}}
	windows := quotaWindows(record.Limits)
	if len(windows) != 3 || windows[0].Label != "5h" || windows[1].Label != "7d" {
		t.Fatal(windows)
	}
	line := quotaLine(windows[0], 120, now)
	if !strings.Contains(line, "━") || !strings.Contains(line, "─") || strings.Contains(line, "█") || !strings.Contains(line, "81%") {
		t.Fatal(line)
	}
	if resetLabel(pointer(now.Unix()-1), now) != "resets now" {
		t.Fatal("reset label")
	}
	keys, remaining := parseKeys([]byte("\x1b[A\x1b[Bw\r"), false)
	if len(remaining) != 0 || !reflect.DeepEqual(keys, []string{"up", "down", "w", "enter"}) {
		t.Fatal(keys, remaining)
	}
}

func helperCLI(t *testing.T, a *App) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "fake-codex")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	text := "#!/bin/sh\nexport XSWAP_TEST_SERVER=1\nexec " + quote(binary) + " -test.run=TestHelperProcess -- \"$@\"\n"
	if err = os.WriteFile(script, []byte(text), 0700); err != nil {
		t.Fatal(err)
	}
	writeJSON(filepath.Join(a.Root, "installation.json"), map[string]string{"cli": script})
}
func TestHelperProcess(t *testing.T) {
	if os.Getenv("XSWAP_TEST_SERVER") != "1" {
		return
	}
	if pidfile := os.Getenv("XSWAP_TEST_PIDFILE"); pidfile != "" {
		os.WriteFile(pidfile, []byte(strconv.Itoa(os.Getpid())), 0600)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		json.Unmarshal(scanner.Bytes(), &request)
		if request.Method == "initialized" {
			continue
		}
		var result any = map[string]any{}
		switch request.Method {
		case "account/read":
			result = map[string]any{"account": Account{Type: "chatgpt", Email: "test@example.com", Plan: "plus"}}
		case "account/rateLimits/read":
			result = quota(5, 65, time.Now().Unix()).Limits
		}
		encoder.Encode(map[string]any{"method": "account/updated"})
		encoder.Encode(map[string]any{"id": request.ID, "result": result})
	}
	os.Exit(0)
}
func TestOfficialQuotaProtocolAndCancellation(t *testing.T) {
	a := fixture(t)
	helperCLI(t, a)
	record, err := a.readLimits(context.Background(), "default")
	if err != nil || record.Account.Email != "test@example.com" {
		t.Fatal(record, err)
	}
	if score, ok := quotaScore(record, time.Now().Unix(), 120); !ok || score != 65 {
		t.Fatal(score, ok)
	}
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("XSWAP_TEST_PIDFILE", pidfile)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = a.readLimits(ctx, "default")
	if err == nil {
		t.Fatal("query did not cancel")
	}
	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(data))
	if err = syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatal("query left child alive", pid, err)
	}
}

func TestDaemonHelperProcess(t *testing.T) {
	if os.Getenv("XSWAP_TEST_DAEMON") != "1" {
		return
	}
	if err := newApp().daemon(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestDaemonSingletonAndStop(t *testing.T) {
	a := fixture(t)
	helperCLI(t, a)
	s, _ := a.settings()
	s.Auto.Enabled = true
	s.Auto.Interval = 10
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), s); err != nil {
		t.Fatal(err)
	}
	binary, _ := os.Executable()
	start := func() *exec.Cmd {
		cmd := exec.Command(binary, "-test.run=TestDaemonHelperProcess")
		cmd.Env = envWith(envWith(os.Environ(), "CODEX_SWAP_HOME", a.Root), "XSWAP_TEST_DAEMON", "1")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	first := start()
	defer func() { first.Process.Kill(); first.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for !exists(filepath.Join(a.Root, "auto-daemon.json")) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !a.daemonRunning() {
		t.Fatal("daemon did not acquire singleton lock")
	}
	second := start()
	done := make(chan error, 1)
	go func() { done <- second.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		second.Process.Kill()
		t.Fatal("duplicate daemon did not stop")
	}
	if err := a.configureAuto(false, 0, 0); err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Wait() }()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not stop after auto off")
	}
	if a.daemonRunning() {
		t.Fatal("daemon retained singleton lock")
	}
}
