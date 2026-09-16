//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
func terminateProcess(cmd *exec.Cmd, force bool) error { return cmd.Process.Kill() }
