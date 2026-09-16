//go:build !windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPanelActionHelperProcess(t *testing.T) {
	if os.Getenv("XSWAP_TEST_PANEL") != "1" {
		return
	}
	a := fixture(t)
	helperCLI(t, a)
	if err := writeJSON(a.Root+"/update.json", map[string]string{"Repository": "owner/xswap"}); err != nil {
		t.Fatal(err)
	}
	a.HTTPClient = &http.Client{Transport: fakeTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v999.0.0"}`))}, nil
	})}
	action, err := a.panel("home", "", 60)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("XSWAP_PANEL_ACTION:" + action)
}

func TestUpdateSelectionLeavesRealTerminalPanel(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Fatal("terminal integration tests require the standard script utility")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(ctx, "script", "-q", "/dev/null", binary, "-test.run=^TestPanelActionHelperProcess$")
	} else {
		quoted := "'" + strings.ReplaceAll(binary, "'", "'\"'\"'") + "'"
		cmd = exec.CommandContext(ctx, "script", "-q", "-e", "-c", quoted+" -test.run=^TestPanelActionHelperProcess$", "/dev/null")
	}
	cmd.Env = envWith(envWith(os.Environ(), "XSWAP_TEST_PANEL", "1"), "TERM", "xterm-256color")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); cmd.Wait() }()
	chunks := make(chan []byte, 32)
	go func() {
		defer close(chunks)
		b := make([]byte, 4096)
		for {
			n, err := stdout.Read(b)
			if n > 0 {
				select {
				case chunks <- append([]byte{}, b[:n]...):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	var output bytes.Buffer
	selected := false
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("panel exited without update action: %s", output.String())
			}
			output.Write(chunk)
			if !selected && strings.Contains(output.String(), "Update version…") {
				// Exercise the actual raw-terminal input and event loop, not only Panel.key.
				if _, err := io.WriteString(stdin, strings.Repeat("\x1b[B", 6)+"\r"); err != nil {
					t.Fatal(err)
				}
				selected = true
			}
			if strings.Contains(output.String(), "XSWAP_PANEL_ACTION:update") {
				if !strings.Contains(output.String(), "\x1b[?25h\x1b[?1049l") {
					t.Fatal("terminal was not restored before returning the update action")
				}
				return
			}
		case <-ctx.Done():
			t.Fatalf("update selection did not leave the terminal panel (menu observed: %t)", selected)
		}
	}
}
