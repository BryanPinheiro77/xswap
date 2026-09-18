//go:build windows

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const batchPercent = "XSWAP_BATCH_LITERAL_PERCENT"

func readStdin(buffer []byte) (int, error) { return os.Stdin.Read(buffer) }
func isRunnable(info os.FileInfo) bool     { return !info.IsDir() }
func processCommand(binary string, args ...string) *exec.Cmd {
	extension := strings.ToLower(filepath.Ext(binary))
	if extension != ".cmd" && extension != ".bat" {
		return exec.Command(binary, args...)
	}
	commandLine := quoteBatchArgument(binary)
	for _, arg := range args {
		commandLine += " " + quoteBatchArgument(arg)
	}
	cmd := exec.Command("cmd.exe")
	cmd.Env = envWith(os.Environ(), batchPercent, "%")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c "` + commandLine + `"`}
	return cmd
}
func quoteBatchArgument(value string) string {
	value = strings.ReplaceAll(value, `%`, `%`+batchPercent+`%`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
func setProcessEnvironment(cmd *exec.Cmd, env []string) {
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, batchPercent+"=") {
			env = envWith(env, batchPercent, "%")
			break
		}
	}
	cmd.Env = env
}
func replaceProcess(binary string, args, env []string) error {
	cmd := processCommand(binary, args...)
	setProcessEnvironment(cmd, env)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func configureProcess(cmd *exec.Cmd)                   {}
func configureDaemon(cmd *exec.Cmd)                    {}
func configureManagedProcess(cmd *exec.Cmd)            {}
func terminateProcess(cmd *exec.Cmd, force bool) error { return cmd.Process.Kill() }
func terminateManagedProcess(cmd *exec.Cmd, force bool) error {
	return cmd.Process.Kill()
}
func managedProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	output, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH", "/FO", "CSV").Output()
	return err == nil && bytes.Contains(output, []byte(`"`+strconv.Itoa(pid)+`"`))
}
func stopManagedProcess(pid int) error {
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T").Run()
}

func sessionLockActive(path string) (bool, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	var overlapped windows.Overlapped
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err == nil {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
		return false, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return true, nil
	}
	return false, err
}
