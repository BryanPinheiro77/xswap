//go:build !windows

package main

import (
	"bufio"
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
	if os.Getenv("XSWAP_TEST_ACTION") == "remove" {
		ready(t, a, "work")
	}
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
	if strings.HasPrefix(action, "remove:") {
		name := strings.TrimPrefix(action, "remove:")
		if _, err = a.remove(name); err != nil {
			t.Fatal(err)
		}
		fmt.Println("XSWAP_PANEL_ACCOUNTS:" + strings.Join(a.names(), ","))
	}
	if os.Getenv("XSWAP_TEST_ACTION") == "add" {
		fmt.Println("XSWAP_NEXT_PROMPT")
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("XSWAP_NEXT_ANSWER:" + strings.TrimSpace(answer))
	}
}

func TestUpdateSelectionLeavesRealTerminalPanel(t *testing.T) {
	testPanelInputHandoff(t, "update")
}

func TestAddSelectionHandsInputToNextPrompt(t *testing.T) {
	testPanelInputHandoff(t, "add")
}

func TestRemoveSelectionAcceptsOneConfirmationKey(t *testing.T) {
	testPanelInputHandoff(t, "remove")
}

func testPanelInputHandoff(t *testing.T, action string) {
	t.Helper()
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
	cmd.Env = envWith(cmd.Env, "XSWAP_TEST_ACTION", action)
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
	accountSelected := false
	confirmed := false
	answered := false
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("panel exited without update action: %s", output.String())
			}
			output.Write(chunk)
			if !selected && strings.Contains(output.String(), "Update version…") {
				// Exercise selection and confirmation in the actual raw-terminal event loop.
				down := 6
				if action == "add" {
					down = 3
				} else if action == "remove" {
					down = 7
				}
				if _, err := io.WriteString(stdin, strings.Repeat("\x1b[B", down)+"\r"); err != nil {
					t.Fatal(err)
				}
				selected = true
			}
			if action == "remove" && selected && !accountSelected && strings.Contains(output.String(), "remove account") {
				if _, err := io.WriteString(stdin, "\x1b[B\r"); err != nil {
					t.Fatal(err)
				}
				accountSelected = true
			}
			if selected && !confirmed && strings.Contains(output.String(), "Install the available XSwap update?") {
				if _, err := io.WriteString(stdin, "y"); err != nil {
					t.Fatal(err)
				}
				confirmed = true
			}
			if action == "remove" && accountSelected && !confirmed && strings.Contains(output.String(), "Remove work from the account list?") {
				if _, err := io.WriteString(stdin, "y"); err != nil {
					t.Fatal(err)
				}
				confirmed = true
			}
			if action == "add" && !answered && strings.Contains(output.String(), "XSWAP_NEXT_PROMPT") {
				// Send exactly one line after the terminal is restored. A leaked raw
				// reader would consume it and leave the next prompt blocked.
				if _, err := io.WriteString(stdin, "first-input\n"); err != nil {
					t.Fatal(err)
				}
				answered = true
			}
			finished := strings.Contains(output.String(), "XSWAP_PANEL_ACTION:update")
			if action == "add" {
				finished = strings.Contains(output.String(), "XSWAP_NEXT_ANSWER:first-input")
			} else if action == "remove" {
				finished = strings.Contains(output.String(), "XSWAP_PANEL_ACCOUNTS:default")
			}
			if finished {
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
