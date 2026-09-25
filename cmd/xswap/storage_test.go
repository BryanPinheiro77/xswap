package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSelectionRejectsMissingLoginAndInvalidAccounts(t *testing.T) {
	a := fixture(t)
	if _, err := a.create("pending"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pending", "missing", "../outside"} {
		if err := a.selectAccount(name); err == nil {
			t.Fatal("selected", name)
		}
		if a.selected() != "default" {
			t.Fatal("failed selection changed active account")
		}
	}
	if err := atomicWrite(filepath.Join(a.Root, "active"), []byte("../invalid\n")); err != nil {
		t.Fatal(err)
	}
	if a.selected() != "default" {
		t.Fatal("invalid selection did not fall back")
	}
	if err := a.setEnabled("missing", false); err == nil {
		t.Fatal("disabled nonexistent account")
	}
}

func TestSettingsRejectCorruptionAndInvalidConfiguration(t *testing.T) {
	for _, data := range []string{"{", `{"auto":{"threshold":0}}`, `{"auto":{"threshold":101}}`, `{"auto":{"interval":1}}`, `{"auto":{"cooldown":-1}}`, `{"auto":{"margin":-1}}`} {
		t.Run(data, func(t *testing.T) {
			a := fixture(t)
			if err := atomicWrite(filepath.Join(a.Root, "settings.json"), []byte(data)); err != nil {
				t.Fatal(err)
			}
			if _, err := a.settings(); err == nil {
				t.Fatal("accepted invalid settings")
			}
			if err := a.setEnabled("default", false); err == nil {
				t.Fatal("overwrote invalid settings")
			}
		})
	}
	a := fixture(t)
	if err := atomicWrite(filepath.Join(a.Root, "settings.json"), []byte(`{"disabled":["default"]}`)); err != nil {
		t.Fatal(err)
	}
	s, err := a.settings()
	if err != nil || s.Auto.Threshold != 90 || s.Auto.Interval != 60 || !disabled(s, "default") {
		t.Fatal("legacy settings lost defaults", s, err)
	}
}

func TestAtomicWritesAndLockFailuresPreserveData(t *testing.T) {
	a := fixture(t)
	target := filepath.Join(a.Root, "value")
	if err := atomicWrite(target, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(target, []byte("second")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "second" {
		t.Fatal(string(data), err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private write permissions", err)
	}
	if err := atomicWrite(filepath.Join(target, "child"), []byte("bad")); err == nil {
		t.Fatal("wrote through regular-file parent")
	}
	data, _ = os.ReadFile(target)
	if string(data) != "second" {
		t.Fatal("damaged parent file")
	}
	if err := writeJSON(target, make(chan int)); err == nil {
		t.Fatal("accepted non-JSON value")
	}
	unlock, err := a.lock(context.Background(), "contended.lock")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := a.lock(ctx, "contended.lock"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("lock ignored deadline", err)
	}
	unlock()
	unlock, err = a.lock(context.Background(), "contended.lock")
	if err != nil {
		t.Fatal("lock was not released", err)
	}
	unlock()
	if _, err := a.lock(context.Background(), "value/child.lock"); err == nil {
		t.Fatal("lock accepted file parent")
	}
}

func TestLockRecoversAbandonedOwnersAndLegacyDirectories(t *testing.T) {
	a := fixture(t)
	if err := os.MkdirAll(a.Root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		owner string
		age   time.Duration
	}{
		{name: "dead-owner", owner: "99999999", age: 3 * time.Second},
		{name: "legacy-empty", age: 25 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(a.Root, tc.name+".lockdir")
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if tc.owner != "" {
				if err := os.WriteFile(filepath.Join(path, "owner"), []byte(tc.owner), 0600); err != nil {
					t.Fatal(err)
				}
			}
			old := time.Now().Add(-tc.age)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			unlock, err := a.lock(ctx, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(path, "owner"))
			if err != nil || string(data) != strconv.Itoa(os.Getpid()) {
				t.Fatal("recovered lock has no current owner", err)
			}
			unlock()
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("lock directory remained after unlock", err)
			}
		})
	}
}

func TestLockDoesNotReclaimLiveOwner(t *testing.T) {
	a := fixture(t)
	if err := os.MkdirAll(a.Root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.Root, "live.lockdir")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	owner := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(filepath.Join(path, "owner"), []byte(owner), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := a.lock(ctx, "live"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("reclaimed a live owner's lock", err)
	}
	data, err := os.ReadFile(filepath.Join(path, "owner"))
	if err != nil || string(data) != owner {
		t.Fatal("changed a live owner's lock", err)
	}
}

func TestRemovalFailuresPreserveProfile(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	home, _ := a.profile("work")
	if err := atomicWrite(filepath.Join(a.Root, "settings.json"), []byte("{")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.remove("work"); err == nil {
		t.Fatal("removed with corrupt settings")
	}
	if !exists(filepath.Join(home, "auth.json")) {
		t.Fatal("profile moved despite invalid settings")
	}
	if err := os.Remove(filepath.Join(a.Root, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(a.Root, "removed"), []byte("block archive directory")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.remove("work"); err == nil {
		t.Fatal("removed into blocked archive")
	}
	if !exists(filepath.Join(home, "auth.json")) {
		t.Fatal("failed archival lost profile")
	}
	if _, err := a.remove("missing"); err == nil {
		t.Fatal("removed nonexistent account")
	}
	if _, err := a.remove("../escape"); err == nil {
		t.Fatal("removed invalid account")
	}
}

func TestRemovalRejectsSymlinkProfiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated Windows privileges")
	}
	a := fixture(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(a.Root, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(a.Root, "profiles", "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.remove("linked"); err == nil {
		t.Fatal("removed symlink profile")
	}
	if !exists(outside) {
		t.Fatal("removed external directory")
	}
}

func TestDisplayNamePersistenceAndFallback(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	s, err := a.settings()
	if err != nil || s.DisplayNames == nil {
		t.Fatal("missing display name settings", s, err)
	}
	if err := a.setDisplayName("work", "Work account"); err != nil {
		t.Fatal(err)
	}
	s, err = a.settings()
	if err != nil || s.DisplayNames["work"] != "Work account" {
		t.Fatal(s, err)
	}
	if err := a.setDisplayName("work", ""); err != nil {
		t.Fatal(err)
	}
	s, err = a.settings()
	if err != nil || len(s.DisplayNames) != 0 {
		t.Fatal(s, err)
	}
	if err := a.setDisplayName("missing", "Nope"); err == nil {
		t.Fatal("accepted missing account")
	}
	if err := a.setDisplayName("work", strings.Repeat("x", 65)); err == nil {
		t.Fatal("accepted oversized display name")
	}
}
