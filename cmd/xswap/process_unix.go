//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func readStdin(buffer []byte) (int, error) { return syscall.Read(int(os.Stdin.Fd()), buffer) }

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func configureDaemon(cmd *exec.Cmd)  { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func terminateProcess(cmd *exec.Cmd, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-cmd.Process.Pid, signal)
}
