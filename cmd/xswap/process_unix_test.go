//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSessionLockActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	active, err := sessionLockActive(path)
	if err != nil || !active {
		t.Fatal("held writer lock was not detected", active, err)
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	active, err = sessionLockActive(path)
	if err != nil || active {
		t.Fatal("released writer lock was reported active", active, err)
	}
}
