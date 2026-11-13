package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Completion metadata follows the public command parser. Internal commands and
// arguments forwarded to Codex are deliberately absent.
type completionSpec struct {
	accounts bool
	argument bool
	noHelp   bool
	flags    string
	children map[string]completionSpec
}

var completionCommands = map[string]completionSpec{
	"add":        {argument: true, flags: "--label --device-auth"},
	"rename":     {accounts: true, flags: "--label"},
	"login":      {accounts: true, flags: "--device-auth"},
	"list":       {},
	"current":    {},
	"switch":     {accounts: true},
	"run":        {accounts: true, noHelp: true},
	"status":     {accounts: true},
	"limits":     {accounts: true, flags: "--all"},
	"usage":      {accounts: true, flags: "--all"},
	"watch":      {accounts: true, flags: "--interval --once"},
	"disable":    {accounts: true},
	"enable":     {accounts: true},
	"remove":     {accounts: true, flags: "--yes"},
	"install":    {},
	"uninstall":  {},
	"version":    {},
	"doctor":     {},
	"export":     {argument: true},
	"import":     {argument: true, flags: "--skip-existing --apply-auto --dry-run"},
	"update":     {flags: "--check --yes --repo"},
	"help":       {},
	"completion": {children: map[string]completionSpec{"bash": {noHelp: true}, "zsh": {noHelp: true}, "fish": {noHelp: true}}},
	"project": {children: map[string]completionSpec{
		"use":     {accounts: true, flags: "--path"},
		"switch":  {accounts: true, flags: "--path --sessions --yes"},
		"clear":   {flags: "--path"},
		"current": {flags: "--path"},
	}},
	"service": {noHelp: true, children: map[string]completionSpec{"install": {noHelp: true}, "status": {noHelp: true}, "logs": {noHelp: true}, "uninstall": {noHelp: true}}},
	"auto": {flags: "--once --dry-run --threshold --threshold-5h --threshold-7d --interval", children: map[string]completionSpec{
		"on": {}, "off": {}, "status": {}, "view": {}, "once": {},
	}},
}

// The last word is always the current prefix, including an empty word after a
// space. Only directory entries and registration metadata are inspected for names.
func (a *App) completions(words []string) []string {
	if len(words) == 0 {
		words = []string{""}
	}
	prefix := words[len(words)-1]
	previous := words[:len(words)-1]
	commands := completionCommands
	spec := completionSpec{children: commands}
	used := map[string]bool{}
	accountSeen := false
	command := ""
	for i := 0; i < len(previous); i++ {
		word := previous[i]
		if word == "--" {
			return nil
		}
		if strings.HasPrefix(word, "-") {
			if !containsCompletionFlag(spec, word) {
				return nil
			}
			used[word] = true
			if valueOption(strings.TrimPrefix(word, "--")) {
				i++
				if i == len(previous) {
					return filterCompletions(completionValues(word), prefix)
				}
			}
			continue
		}
		if child, ok := commands[word]; ok {
			if command == "" {
				command = word
			}
			// Auto options apply both before and after its operation.
			if command == "auto" && word != "view" && child.flags == "" {
				child.flags = spec.flags
			}
			spec = child
			commands = spec.children
			continue
		}
		if (!spec.accounts && !spec.argument) || accountSeen {
			return nil
		}
		accountSeen = true
	}
	if command == "run" && accountSeen {
		return nil
	}
	var candidates []string
	for name := range commands {
		candidates = append(candidates, name)
	}
	if spec.accounts && !accountSeen && !used["--all"] && !strings.HasPrefix(prefix, "-") {
		candidates = append(candidates, a.names()...)
	}
	if prefix == "" || strings.HasPrefix(prefix, "-") {
		for _, flag := range completionFlags(spec) {
			if !used[flag] && !(flag == "--all" && accountSeen) {
				candidates = append(candidates, flag)
			}
		}
	}
	return filterCompletions(candidates, prefix)
}

func containsCompletionFlag(spec completionSpec, flag string) bool {
	for _, candidate := range completionFlags(spec) {
		if candidate == flag {
			return true
		}
	}
	return false
}

func completionFlags(spec completionSpec) []string {
	flags := spec.flags
	if !spec.noHelp {
		flags += " --help -h"
	}
	return strings.Fields(flags)
}

// valueOption is shared by argument parsing and completion so value-taking
// options cannot accidentally consume an account name differently.
func valueOption(key string) bool {
	switch key {
	case "threshold", "threshold-5h", "threshold-7d", "interval", "repo", "label", "path", "sessions":
		return true
	}
	return false
}

func completionValues(flag string) []string {
	switch flag {
	case "--sessions":
		return []string{"recent", "24h", "7d", "all"}
	case "--threshold-5h", "--threshold-7d":
		return []string{"global"}
	}
	return nil
}

func filterCompletions(values []string, prefix string) []string {
	var result []string
	seen := map[string]bool{}
	for _, value := range values {
		if strings.HasPrefix(value, prefix) && !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	sort.Strings(result)
	return result
}

func (a *App) writeCompletions(out io.Writer, words []string) error {
	for _, value := range a.completions(words) {
		if _, err := fmt.Fprintln(out, value); err != nil {
			return err
		}
	}
	return nil
}

func completionCommand(out io.Writer, args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, "usage: xswap completion bash|zsh|fish")
		return err
	}
	if len(args) != 1 {
		return errors.New("usage: xswap completion bash|zsh|fish")
	}
	var script string
	switch args[0] {
	case "bash":
		script = bashCompletion
	case "zsh":
		script = zshCompletion
	case "fish":
		script = fishCompletion
	default:
		return errors.New("unsupported shell; use bash, zsh, or fish")
	}
	_, err := io.WriteString(out, script)
	return err
}

const bashCompletion = `# XSwap bash completion. Source this file from ~/.bashrc.
_xswap_complete() {
    local candidate
    COMPREPLY=()
    while IFS= read -r candidate; do
        COMPREPLY+=("$candidate")
    done < <("${COMP_WORDS[0]}" __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)
}
complete -F _xswap_complete xswap codex-swap
`

const zshCompletion = `#compdef xswap codex-swap
_xswap_complete() {
    local -a candidates
    candidates=("${(@f)$("${words[1]}" __complete "${(@)words[2,CURRENT]}" 2>/dev/null)}")
    (( ${#candidates} )) && compadd -- "${candidates[@]}"
}
compdef _xswap_complete xswap codex-swap
`

const fishCompletion = `# XSwap fish completion.
function __xswap_complete
    set -l words (commandline -opc)
    set -l current (commandline -ct)
    command $words[1] __complete $words[2..-1] "$current" 2>/dev/null
end
complete -c xswap -f -a '(__xswap_complete)'
complete -c codex-swap -f -a '(__xswap_complete)'
`
