package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCompletionContexts(t *testing.T) {
	a := fixture(t)
	for _, name := range []string{"work", "weekend", "personal"} {
		if _, err := a.create(name); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		words []string
		want  []string
	}{
		{[]string{"sw"}, []string{"switch"}},
		{[]string{"switch", "w"}, []string{"weekend", "work"}},
		{[]string{"run", "w"}, []string{"weekend", "work"}},
		{[]string{"run", "work", ""}, nil},
		{[]string{"run", "work", "--", ""}, nil},
		{[]string{"project", ""}, []string{"--help", "-h", "clear", "current", "switch", "use"}},
		{[]string{"project", "use", "w"}, []string{"weekend", "work"}},
		{[]string{"project", "switch", "--path", "/a project", "w"}, []string{"weekend", "work"}},
		{[]string{"project", "switch", "work", "--sessions", ""}, []string{"24h", "7d", "all", "recent"}},
		{[]string{"project", "switch", "--sessions", "7"}, []string{"7d"}},
		{[]string{"auto", "on", "--threshold-5h", "g"}, []string{"global"}},
		{[]string{"auto", "--threshold", "80", "o"}, []string{"off", "on", "once"}},
		{[]string{"watch", "--interval", ""}, nil},
		{[]string{"rename", "--label", ""}, nil},
		{[]string{"add", "w"}, nil},
		{[]string{"add", "new-account", "--d"}, []string{"--device-auth"}},
		{[]string{"auto", "view", "--t"}, nil},
		{[]string{"switch", "work", "w"}, nil},
		{[]string{"limits", "--all", "w"}, nil},
		{[]string{"limits", "work", "--a"}, nil},
		{[]string{"watch", "--once", "--o"}, nil},
		{[]string{"service", ""}, []string{"install", "logs", "status", "uninstall"}},
		{[]string{"completion", ""}, []string{"--help", "-h", "bash", "fish", "zsh"}},
		{[]string{"completion", "bash", ""}, nil},
		{[]string{"unknown", ""}, nil},
		{[]string{"switch", "--unknown", ""}, nil},
		{[]string{"switch", "--", ""}, nil},
	} {
		t.Run(strings.Join(test.words, "/"), func(t *testing.T) {
			if got := a.completions(test.words); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("completions(%q) = %q, want %q", test.words, got, test.want)
			}
		})
	}
	for _, command := range []string{"rename", "login", "switch", "run", "status", "limits", "usage", "watch", "disable", "enable", "remove"} {
		if got := a.completions([]string{command, "w"}); !reflect.DeepEqual(got, []string{"weekend", "work"}) {
			t.Fatalf("%s accounts = %q", command, got)
		}
	}
}

func TestCompletionsAreOfflineAndDoNotReadCredentials(t *testing.T) {
	a := fixture(t)
	for _, name := range []string{"pending", "legacy", "orphan", "bad name"} {
		dir := filepath.Join(a.Root, "profiles", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := atomicWrite(filepath.Join(a.Root, "profiles", "pending", profileMarker), []byte("registered")); err != nil {
		t.Fatal(err)
	}
	// An unreadable auth file is sufficient registration metadata; its contents
	// must never be opened to produce account suggestions.
	if err := os.WriteFile(filepath.Join(a.Root, "profiles", "legacy", "auth.json"), []byte("fake-private-credential"), 0000); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(a.Root, "profiles", "bad name", profileMarker), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(a.Root, "profiles", "pending"), filepath.Join(a.Root, "profiles", "linked")); err != nil && runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(a.Root, "settings.json"), []byte("malformed settings")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(a.Root)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.writeCompletions(&out, []string{"switch", ""}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "--help\n-h\ndefault\nlegacy\npending\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	after, _ := os.ReadDir(a.Root)
	if !reflect.DeepEqual(before, after) || exists(filepath.Join(a.Root, "active")) || exists(filepath.Join(a.Root, "daemon")) {
		t.Fatal("completion mutated manager state")
	}
}

func TestCompletionScriptsAndValidation(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var out bytes.Buffer
			if err := completionCommand(&out, []string{shell}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "__complete") || !strings.Contains(out.String(), "codex-swap") {
				t.Fatal("missing dynamic completion or compatibility command")
			}
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s is not installed", shell)
			}
			cmd := exec.Command(path, "-n")
			cmd.Stdin = &out
			if result, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("invalid %s script: %s: %v", shell, result, err)
			}
		})
	}
	for _, args := range [][]string{nil, {"powershell"}, {"bash", "extra"}} {
		var out bytes.Buffer
		if err := completionCommand(&out, args); err == nil || out.Len() != 0 {
			t.Fatalf("accepted %q", args)
		}
	}
}

func TestCompletionShellCallbacksPreserveWords(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell callbacks")
	}
	directory := t.TempDir()
	cli := filepath.Join(directory, "fake xswap")
	capture := filepath.Join(directory, "arguments")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$XSWAP_TEST_COMPLETION_ARGS\"\nprintf '%s\\n' weekend work\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		shell, setup, call string
	}{
		{"bash", bashCompletion, `COMP_WORDS=("$XSWAP_TEST_COMPLETION_CLI" switch "")
COMP_CWORD=2
_xswap_complete
printf '%s\n' "${COMPREPLY[@]}"
`},
		{"zsh", "compdef() { :; }\n" + zshCompletion, `compadd() { shift; printf '%s\n' "$@"; }
words=("$XSWAP_TEST_COMPLETION_CLI" switch "")
CURRENT=3
_xswap_complete
`},
		{"fish", fishCompletion, `function commandline
    if test "$argv[1]" = -opc
        printf '%s\n' "$XSWAP_TEST_COMPLETION_CLI" switch
    else
        printf ''
    end
end
__xswap_complete
`},
	} {
		t.Run(test.shell, func(t *testing.T) {
			path, err := exec.LookPath(test.shell)
			if err != nil {
				t.Skipf("%s is not installed", test.shell)
			}
			args := []string{"-f"}
			if test.shell == "bash" {
				args = []string{"--noprofile", "--norc"}
			} else if test.shell == "fish" {
				args = []string{"--no-config"}
			}
			cmd := exec.Command(path, args...)
			cmd.Stdin = strings.NewReader(test.setup + "\n" + test.call)
			cmd.Env = append(os.Environ(), "XSWAP_TEST_COMPLETION_CLI="+cli, "XSWAP_TEST_COMPLETION_ARGS="+capture)
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != "weekend\nwork\n" {
				t.Fatalf("callback output %q: %v", out, err)
			}
			got, err := os.ReadFile(capture)
			if err != nil || string(got) != "__complete\nswitch\n\n" {
				t.Fatalf("lost word boundary or empty prefix: %q: %v", got, err)
			}
		})
	}
}

func TestCompletionMetadataCoversParserOptions(t *testing.T) {
	seen := map[string]bool{}
	var visit func(map[string]completionSpec)
	visit = func(commands map[string]completionSpec) {
		for _, spec := range commands {
			for _, flag := range strings.Fields(spec.flags) {
				args := []string{flag}
				if valueOption(strings.TrimPrefix(flag, "--")) {
					args = append(args, "value")
				}
				if _, err := parse(args); err != nil {
					t.Errorf("completion flag %s rejected by parser: %v", flag, err)
				}
				seen[flag] = true
			}
			visit(spec.children)
		}
	}
	visit(completionCommands)
	for _, key := range []string{"device-auth", "all", "yes", "once", "dry-run", "check", "threshold", "threshold-5h", "threshold-7d", "interval", "repo", "label", "path", "sessions"} {
		if !seen["--"+key] {
			t.Errorf("parser option --%s has no completion", key)
		}
	}
}
