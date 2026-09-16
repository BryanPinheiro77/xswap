//go:build windows

package main

import (
	"os"
	"os/exec"
)

func readStdin(buffer []byte) (int, error) { return os.Stdin.Read(buffer) }
func isRunnable(info os.FileInfo) bool     { return !info.IsDir() }
func replaceProcess(binary string, args, env []string) error {
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func configureProcess(cmd *exec.Cmd)                   {}
func configureDaemon(cmd *exec.Cmd)                    {}
func terminateProcess(cmd *exec.Cmd, force bool) error { return cmd.Process.Kill() }
