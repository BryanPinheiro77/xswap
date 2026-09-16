//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func readStdin(buffer []byte) (int, error) { return os.Stdin.Read(buffer) }
func isRunnable(info os.FileInfo) bool     { return !info.IsDir() }
func processCommand(binary string, args ...string) *exec.Cmd {
	extension := strings.ToLower(filepath.Ext(binary))
	if extension != ".cmd" && extension != ".bat" {
		return exec.Command(binary, args...)
	}
	values := append([]string{binary}, args...)
	commandLine := ""
	env := os.Environ()
	for index, value := range values {
		key := fmt.Sprintf("XSWAP_BATCH_ARG_%d", index)
		if index > 0 {
			commandLine += " "
		}
		commandLine += `"!` + key + `!"`
		env = envWith(env, key, value)
	}
	cmd := exec.Command("cmd.exe")
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /v:on /s /c "` + commandLine + `"`}
	return cmd
}
func setProcessEnvironment(cmd *exec.Cmd, env []string) {
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "XSWAP_BATCH_ARG_") {
			parts := strings.SplitN(entry, "=", 2)
			env = envWith(env, parts[0], parts[1])
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
