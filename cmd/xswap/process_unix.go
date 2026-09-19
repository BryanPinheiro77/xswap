//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func isRunnable(info os.FileInfo) bool { return !info.IsDir() && info.Mode()&0111 != 0 }
func processCommand(binary string, args ...string) *exec.Cmd {
	return exec.Command(binary, args...)
}
func setProcessEnvironment(cmd *exec.Cmd, env []string) { cmd.Env = env }
func replaceProcess(binary string, args, env []string) error {
	return syscall.Exec(binary, append([]string{binary}, args...), env)
}

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func configureDaemon(cmd *exec.Cmd)  { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// An interactive Codex child must stay in the terminal's foreground process
// group. Moving it to a new group makes shells with job control suspend it on
// the first terminal read.
func configureManagedProcess(*exec.Cmd) {}
func terminateProcess(cmd *exec.Cmd, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-cmd.Process.Pid, signal)
}

func terminateManagedProcess(cmd *exec.Cmd, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(cmd.Process.Pid, signal)
}

func managedProcessAlive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }
func stopManagedProcess(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

func sessionLockActive(path string) (bool, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return false, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return true, nil
	}
	return false, err
}
