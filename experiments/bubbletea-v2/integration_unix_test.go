//go:build !windows

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestInteractiveSubprocessReturnsTerminalInput(t *testing.T) {
	if testing.Short() {
		t.Skip("terminal integration test")
	}
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("standard script utility is unavailable")
	}
	binary := filepath.Join(t.TempDir(), "prototype")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build prototype: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	quoted := "'" + strings.ReplaceAll(binary, "'", "'\"'\"'") + "'"
	run := "stty rows 24 cols 80; exec " + quoted
	var command *exec.Cmd
	if runtime.GOOS == "darwin" {
		command = exec.CommandContext(ctx, "script", "-q", "/dev/null", "sh", "-c", run)
	} else {
		command = exec.CommandContext(ctx, "script", "-q", "-e", "-c", "sh -c "+shellQuote(run), "/dev/null")
	}
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = command.Stdout
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
	}()
	chunks := make(chan []byte, 32)
	go func() {
		defer close(chunks)
		buffer := make([]byte, 4096)
		for {
			count, readErr := stdout.Read(buffer)
			if count > 0 {
				chunks <- append([]byte{}, buffer[:count]...)
			}
			if readErr != nil {
				return
			}
		}
	}()
	var output bytes.Buffer
	selected, answered := false, false
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("prototype exited before terminal input returned:\n%s", output.String())
			}
			output.Write(chunk)
			text := output.String()
			if !selected && strings.Contains(text, "Run terminal input probe") {
				if _, err = io.WriteString(stdin, "\x1b[B\r"); err != nil {
					t.Fatal(err)
				}
				selected = true
			}
			if selected && !answered && strings.Contains(text, "probe input:") {
				if _, err = io.WriteString(stdin, "bubble-tea-owned-this-line\n"); err != nil {
					t.Fatal(err)
				}
				answered = true
			}
			if answered && strings.Contains(text, "child received: bubble-tea-owned-this-line") && strings.Contains(text, "terminal input returned to Bubble Tea") {
				_, _ = io.WriteString(stdin, "q")
				return
			}
		case <-ctx.Done():
			t.Fatalf("terminal input handoff timed out (selected=%t answered=%t):\n%s", selected, answered, output.String())
		}
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
