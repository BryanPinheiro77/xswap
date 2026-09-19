package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func (p *Panel) render(a *App, names []string, s Settings, now time.Time) string {
	rows := make([]string, p.Height)
	if p.Height < 20 || p.Width < 50 {
		rows[0] = "Resize the terminal to at least 50×20. Press q to quit."
		return renderRows(rows, p.Width)
	}
	heading := map[string]string{"home": "xswap", "watch": "watching all accounts", "auto": "auto-switch view", "switch": "select account", "project-select": "select project", "session-select": "select sessions to continue", "conflict-source": "resolve divergent session", "project-switch": "select destination account", "rename": "select account to rename", "rename-input": "rename account", "disable": "disable / enable account", "remove": "remove account", "confirm": "confirm removal", "confirm-update": "confirm update", "confirm-project-switch": "review session continuation"}[p.Mode]
	if p.Mode == "home" {
		heading += " " + clean(version)
	}
	if p.Filter != "" && p.Mode == "watch" {
		heading = "watching " + p.Filter
	}
	status := "querying…"
	if !p.Busy {
		status = fmt.Sprintf("refresh in %ds", max(0, int(p.Due.Sub(now).Seconds())))
	}
	if p.Mode == "session-select" {
		status = fmt.Sprintf("%d of %d selected", len(p.selectedSessions()), len(p.HandoffSessions))
	}
	rows[0] = muted + "  " + heading + "  ·  " + status + reset
	if p.Mode == "home" {
		if root, err := projectRoot(currentDirectory()); err == nil {
			projectName := filepath.Base(root)
			if selection, found, selectionErr := a.projectSelection(currentDirectory()); selectionErr == nil && found {
				rows[1] = muted + "  project " + clean(projectName) + "  ·  account " + clean(a.displayName(selection.Account)) + reset
			} else {
				rows[1] = muted + "  project " + clean(projectName) + "  ·  follows global account" + reset
			}
		}
	}
	if p.Mode == "session-select" {
		sources := make([]string, 0, len(p.HandoffSources))
		for _, source := range p.HandoffSources {
			sources = append(sources, clean(a.displayName(source)))
		}
		rows[1] = muted + "  from " + strings.Join(sources, ", ") + "  ·  project " + clean(filepath.Base(p.HandoffProject)) + reset
	}
	lines, chosen := p.accountLines(a, names, s, now)
	if p.Mode == "project-select" {
		lines, chosen = p.projectLines()
	}
	if p.Mode == "session-select" {
		lines, chosen = p.sessionLines(a)
	}
	if p.Mode == "conflict-source" {
		lines, chosen = p.conflictSourceLines(a)
	}
	if p.Mode == "auto" {
		lines = p.autoLines(a, s, now)
	}
	if p.Mode == "confirm" {
		lines = []string{"", bold + "  Remove " + p.Pending + " from the account list?" + reset, muted + "  Credentials and history will be archived locally, not deleted." + reset, "", accent(p.Theme) + "  y Confirm removal   esc Cancel" + reset}
	}
	if p.Mode == "confirm-update" {
		method := "the built-in updater"
		if a.PackageManager == "homebrew" {
			method = "Homebrew"
		}
		lines = []string{"", bold + "  Install the available XSwap update?" + reset, muted + "  The update will be installed with " + method + "." + reset, "", accent(p.Theme) + "  y Confirm update   esc Cancel" + reset}
	}
	if p.Mode == "confirm-project-switch" {
		selected := p.selectedSessions()
		sources := make([]string, 0, len(p.HandoffSources))
		for _, source := range p.HandoffSources {
			sources = append(sources, clean(a.displayName(source)))
		}
		scope := muted + "  Other projects and unselected sessions are not changed." + reset
		if isHomeScope(p.HandoffProject) {
			scope = "\x1b[33m  Global account: new Codex processes in unpinned directories will use this account." + reset
		}
		lines = []string{"", bold + fmt.Sprintf("  Continue %d selected session(s) with %s?", len(selected), clean(a.displayName(p.Pending))) + reset,
			muted + "  Sources: " + strings.Join(sources, ", ") + reset,
			muted + "  Scope: " + clean(filepath.Base(p.HandoffProject)) + reset,
			muted + fmt.Sprintf("  Selected conversations: %d", len(selected)) + reset}
		for _, session := range selected {
			lines = append(lines, "    • "+sessionTitle(session, p.HandoffProject))
		}
		lines = append(lines,
			muted+fmt.Sprintf("  Restart and resume %d currently managed Codex session(s).", p.HandoffManaged)+reset,
			scope, "", accent(p.Theme)+"  enter / y Confirm continuation   esc Back"+reset)
		if p.HandoffArchives > 0 {
			copies := "copy"
			if p.HandoffArchives > 1 {
				copies = "copies"
			}
			warning := fmt.Sprintf("  %d divergent destination %s will be archived before replacement.", p.HandoffArchives, copies)
			lines = append(lines[:len(lines)-2], "\x1b[33m"+warning+reset,
				muted+"  Archives stay private under ~/.codex-swap/session-conflicts."+reset,
				lines[len(lines)-2], lines[len(lines)-1])
		}
		manual := p.selectedUnmanagedSessions()
		if len(manual) > 0 {
			warning := fmt.Sprintf("  %d open session(s) require manual resume.", len(manual))
			lines = append(lines[:len(lines)-2], "\x1b[33m"+warning+reset,
				muted+"  After transfer, close the old Codex process before running codex resume."+reset,
				lines[len(lines)-2], lines[len(lines)-1])
		}
	}
	if p.Mode == "rename-input" {
		lines = append(lines, "", bold+"  Display name: "+p.Input+"█"+reset, muted+"  Type a name, or leave it empty to use the e-mail. Enter saves; Esc cancels."+reset)
	}
	available := p.Height - 6
	if p.Mode == "home" {
		menuItems := p.menu()
		available = max(1, p.Height-len(menuItems)-11)
	}
	if p.Mode == "switch" || p.Mode == "project-select" || p.Mode == "session-select" || p.Mode == "conflict-source" || p.Mode == "project-switch" || p.Mode == "disable" || p.Mode == "remove" {
		if chosen < p.Offset || chosen >= p.Offset+available {
			p.Offset = chosen
		}
	}
	p.Offset = min(p.Offset, max(0, len(lines)-available))
	for i, line := range lines[p.Offset:min(len(lines), p.Offset+available)] {
		rows[2+i] = line
	}
	if p.Mode == "home" {
		menuItems := p.menu()
		menuRow := min(3+min(len(lines), available), p.Height-len(menuItems)-8)
		menuRow = max(3, menuRow)
		rows[menuRow] = muted + strings.Repeat("─", p.Width-1) + reset
		rows[menuRow+2] = muted + "  menu" + reset
		for i, item := range menuItems {
			text := "   " + item
			if i == p.MenuCursor {
				text = highlight + accent(p.Theme) + " ▌ " + bold + item + strings.Repeat(" ", max(0, p.Width-len([]rune(item))-4)) + reset
			}
			rows[menuRow+4+i] = text
		}
	}
	rows[p.Height-3] = "  \x1b[33m" + clean(p.Message) + reset
	footer := "  s Switch   r Refresh   esc Back   t Theme   q Quit"
	if p.Mode == "home" {
		footer = "  s Switch accounts   w Watch   u Auto-switch   q Quit   ^t Theme"
	}
	if p.Mode == "auto" {
		footer = "  e Enable / stop   +/- Threshold   r Refresh   esc Back   q Quit"
	}
	if p.Mode == "rename-input" {
		footer = "  Type display name   enter Save   backspace Delete   esc Cancel   q Quit"
	}
	if p.Mode == "session-select" {
		footer = "  space Toggle   a Select / unselect all   enter Continue   esc Back"
	}
	if p.Mode == "conflict-source" {
		footer = "  enter Keep this account copy   esc Back   q Quit"
	}
	if p.Mode == "project-select" {
		footer = "  enter Select project   esc Back   q Quit"
	}
	if p.Mode == "confirm-project-switch" {
		footer = "  enter / y Confirm continuation   esc Back   q Quit"
	}
	rows[p.Height-2] = accent(p.Theme) + footer + reset
	rows[p.Height-1] = muted + "  ↑/↓ Navigate  ·  Enter Select  ·  Percentages show quota usage" + reset
	return renderRows(rows, p.Width)
}
func renderRows(rows []string, width int) string {
	var b strings.Builder
	for i, row := range rows {
		b.WriteString(fitANSI(row, width-1))
		if i < len(rows)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
