//go:build windows

package main

import (
	"os"
	"os/exec"
)

func readStdin(buffer []byte) (int, error) { return os.Stdin.Read(buffer) }

func configureProcess(cmd *exec.Cmd)                   {}
func configureDaemon(cmd *exec.Cmd)                    {}
func terminateProcess(cmd *exec.Cmd, force bool) error { return cmd.Process.Kill() }
