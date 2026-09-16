//go:build windows

package main

import (
	"os"
)

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(os.Signal(nil))
	if err != nil {
		return false
	}
	return true
}
