//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func readStdin(buffer []byte) (int, error) { return syscall.Read(int(os.Stdin.Fd()), buffer) }
func isRunnable(info os.FileInfo) bool     { return !info.IsDir() && info.Mode()&0111 != 0 }
func processCommand(binary string, args ...string) *exec.Cmd {
	return exec.Command(binary, args...)
}
func setProcessEnvironment(cmd *exec.Cmd, env []string) { cmd.Env = env }
func replaceProcess(binary string, args, env []string) error {
	return syscall.Exec(binary, append([]string{binary}, args...), env)
}

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func configureDaemon(cmd *exec.Cmd)  { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func terminateProcess(cmd *exec.Cmd, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-cmd.Process.Pid, signal)
}

func managedProcessAlive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }
func stopManagedProcess(pid int) error { return syscall.Kill(-pid, syscall.SIGTERM) }
