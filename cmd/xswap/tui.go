package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const reset = "\x1b[0m"
const muted = "\x1b[38;5;244m"
const bold = "\x1b[1m"
const highlight = "\x1b[48;5;236m"

func accent(theme int) string { return []string{"\x1b[38;5;208m", "\x1b[36m", "\x1b[35m"}[theme%3] }
func usageColor(used *float64) string {
	if used == nil {
		return muted
	}
	if *used >= 90 {
		return "\x1b[31m"
	}
	if *used >= 70 {
		return "\x1b[33m"
	}
	return "\x1b[32m"
}
func interactive() bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
func terminalSize() (int, int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err == nil && h > 0 && w > 0 {
		return h, w
	}
	return 30, 120
}
func (a *App) dashboard(mode, filter string, interval int) error {
	if !interactive() {
		return errors.New("the panel needs an interactive terminal; use xswap watch --once or xswap limits")
	}
	for {
		action, err := a.panel(mode, filter, interval)
		if err != nil {
			return err
		}
		if action == "update" {
			fmt.Println("Checking for updates…")
			installed, updateErr := a.updateCommand(Options{Flags: map[string]bool{"yes": true}})
			if updateErr != nil {
				fmt.Println("Update failed:", updateErr)
			}
			if installed {
				return replaceProcess(a.Binary, nil, os.Environ())
			}
			fmt.Print("\nPress Enter to return to the menu.")
			bufio.NewReader(os.Stdin).ReadString('\n')
			mode = "home"
			continue
		}
		if strings.HasPrefix(action, "remove:") {
			name := strings.TrimPrefix(action, "remove:")
			fmt.Printf("Removing %s and archiving its local data…\n", name)
			archive, removeErr := a.remove(name)
			if removeErr != nil {
				fmt.Println("Removal failed:", removeErr)
			} else {
				fmt.Println("Account removed. Data archived at", archive)
			}
			fmt.Print("\nPress Enter to return to the menu.")
			bufio.NewReader(os.Stdin).ReadString('\n')
			mode = "home"
			continue
		}
		if strings.HasPrefix(action, "project-handoff:") {
			project, target, selected, actionErr := parseHandoffAction(action)
			fmt.Println("Switching the project account and transferring its conversations…")
			if actionErr != nil {
				fmt.Println("Project switch failed:", actionErr)
			} else if plan, planErr := a.planSelectedProjectHandoff(project, target, selected); planErr != nil {
				fmt.Println("Project switch failed:", planErr)
			} else if result, handoffErr := a.requestProjectHandoff(plan); handoffErr != nil {
				fmt.Println("Project switch failed:", handoffErr)
			} else {
				printProjectHandoffResult(a, result)
			}
			fmt.Print("\nPress Enter to return to the menu.")
			bufio.NewReader(os.Stdin).ReadString('\n')
			mode = "home"
			continue
		}
		if action != "add" {
			return nil
		}
		name, err := a.createNumbered()
		if err != nil {
			return err
		}
		fmt.Printf("Adding %s. Sign in to the account you want to register.\n", name)
		if err = a.login(name, false); err != nil {
			fmt.Println("Login incomplete:", err)
		}
		fmt.Print("\nPress Enter to return to the menu.")
		bufio.NewReader(os.Stdin).ReadString('\n')
		mode = "home"
	}
}

type panelUpdate struct {
	Name   string
	Record Record
	Error  string
	Done   bool
}
type Panel struct {
	Mode, Filter, Message, Pending, Input string
	Cursor, MenuCursor, Offset, Theme     int
	Records                               map[string]Record
	Busy                                  bool
	UpdateAvailable                       bool
	Due                                   time.Time
	Width, Height                         int
	HandoffManaged                        int
	HandoffProject, HandoffSource         string
	HandoffProjects                       []sessionProject
	HandoffSessions                       []codexSession
	HandoffSelected, HandoffRunning       map[string]bool
	HandoffUnmanaged                      map[string]bool
}

func handoffAction(project, target string, sessions []codexSession) string {
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, session.ID)
	}
	encodedProject := base64.RawURLEncoding.EncodeToString([]byte(project))
	return "project-handoff:" + encodedProject + ":" + target + ":" + strings.Join(ids, ",")
}

func parseHandoffAction(action string) (string, string, map[string]bool, error) {
	parts := strings.SplitN(strings.TrimPrefix(action, "project-handoff:"), ":", 3)
	if len(parts) != 3 {
		return "", "", nil, errors.New("session handoff action is invalid")
	}
	projectBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || !filepath.IsAbs(string(projectBytes)) {
		return "", "", nil, errors.New("session handoff project is invalid")
	}
	selected := map[string]bool{}
	for _, id := range strings.Split(parts[2], ",") {
		if sessionIDPattern.MatchString(id) {
			selected[id] = true
		}
	}
	return filepath.Clean(string(projectBytes)), parts[1], selected, nil
}

var menuItems = []string{"Switch account…", "Continue sessions with another account…", "Watch accounts", "Auto-switch view", "Add account…", "Rename account…", "Disable / enable account…", "Remove account…", "Theme…", "Quit"}

func (p *Panel) menu() []string {
	items := []string{}
	for _, item := range menuItems {
		if item == "Remove account…" && p.UpdateAvailable {
			items = append(items, "Update version…")
		}
		items = append(items, item)
	}
	return items
}

func (p *Panel) selectedSessions() []codexSession {
	selected := []codexSession{}
	for _, session := range p.HandoffSessions {
		if p.HandoffSelected[session.ID] {
			selected = append(selected, session)
		}
	}
	return selected
}

func (p *Panel) selectedUnmanagedSessions() []codexSession {
	selected := []codexSession{}
	for _, session := range p.HandoffSessions {
		if p.HandoffSelected[session.ID] && p.HandoffUnmanaged[session.ID] {
			selected = append(selected, session)
		}
	}
	return selected
}

func (p *Panel) useHandoffPlan(plan projectHandoffPlan) {
	p.HandoffProject = plan.Project
	p.HandoffSource = plan.Source
	p.HandoffSessions = plan.Sessions
	p.HandoffSelected = map[string]bool{}
	p.HandoffRunning = map[string]bool{}
	p.HandoffUnmanaged = map[string]bool{}
	for _, session := range plan.Sessions {
		p.HandoffSelected[session.ID] = true
	}
	for _, record := range plan.Managed {
		if record.SessionID != "" {
			p.HandoffRunning[record.SessionID] = true
		}
	}
	for _, session := range plan.Unmanaged {
		p.HandoffUnmanaged[session.ID] = true
	}
}

func (p *Panel) openHandoffProject(a *App, directory string) error {
	plan, err := a.projectHandoffSource(directory)
	if err != nil {
		return err
	}
	if len(plan.Sessions) == 0 {
		return errors.New("no conversations were found for this project in any XSwap account")
	}
	p.useHandoffPlan(plan)
	p.Mode = "session-select"
	p.Cursor = 0
	p.Offset = 0
	p.Message = ""
	return nil
}

func projectLabel(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		cleanHome := filepath.Clean(home)
		cleanPath := filepath.Clean(path)
		if cleanPath == cleanHome {
			return "~"
		}
		if relative, relativeErr := filepath.Rel(cleanHome, cleanPath); relativeErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return filepath.Join("~", relative)
		}
	}
	return filepath.Clean(path)
}

func (p *Panel) projectLines() ([]string, int) {
	lines := []string{}
	chosen := 0
	for index, project := range p.HandoffProjects {
		if index == p.Cursor {
			chosen = len(lines)
		}
		name := filepath.Base(project.Root)
		if isHomeScope(project.Root) {
			name = "Home"
		}
		title := fmt.Sprintf("  %d  %s", index+1, clean(name))
		if index == p.Cursor {
			title = highlight + accent(p.Theme) + " ▌ " + strings.TrimSpace(title) + reset
		}
		conversation := "conversations"
		if project.Count == 1 {
			conversation = "conversation"
		}
		meta := fmt.Sprintf("     %s  ·  %d %s  ·  last used %s", clean(projectLabel(project.Root)), project.Count, conversation, project.Updated.Format("Jan 02 15:04"))
		lines = append(lines, title, muted+meta+reset, "")
	}
	return lines, chosen
}

func sessionTitle(session codexSession, project string) string {
	title := strings.Join(strings.Fields(session.Preview), " ")
	if title == "" {
		location := filepath.Base(project)
		if relative, err := filepath.Rel(project, session.CWD); err == nil && relative != "." && !strings.HasPrefix(relative, "..") {
			location = relative
		}
		title = "Conversation in " + location
	}
	if runes := []rune(title); len(runes) > 72 {
		title = string(runes[:71]) + "…"
	}
	return clean(title)
}

func (p *Panel) sessionLines() ([]string, int) {
	lines := []string{}
	chosen := 0
	for index, session := range p.HandoffSessions {
		if index == p.Cursor {
			chosen = len(lines)
		}
		box := "[ ]"
		if p.HandoffSelected[session.ID] {
			box = "[✓]"
		}
		title := fmt.Sprintf("  %s  %d  %s", box, index+1, sessionTitle(session, p.HandoffProject))
		if index == p.Cursor {
			title = highlight + accent(p.Theme) + " ▌ " + strings.TrimSpace(title) + reset
		}
		meta := session.Updated.Format("Jan 02 15:04")
		if p.HandoffRunning[session.ID] {
			meta += "  ·  running"
		} else if p.HandoffUnmanaged[session.ID] {
			meta += "  ·  open outside XSwap"
		}
		lines = append(lines, title, muted+"       "+meta+reset, "")
	}
	return lines, chosen
}

func quotaLine(item QuotaItem, width int, now time.Time) string {
	label := clean(item.Label)
	if len([]rune(label)) > 16 {
		label = string([]rune(label)[:15]) + "…"
	}
	used := item.Window.Used
	percent := "?"
	if used != nil {
		percent = fmt.Sprintf("%g%%", *used)
	}
	prefix := muted + fmt.Sprintf("    %-12s ", label) + reset
	if width < 70 {
		return prefix + usageColor(used) + percent + reset + muted + "  " + resetLabel(item.Window.Resets, now) + reset
	}
	length := min(32, max(10, width-63))
	filled := 0
	if used != nil && !math.IsNaN(*used) {
		filled = int(math.Round(max(0, min(100, *used)) * float64(length) / 100))
	}
	bar := usageColor(used) + strings.Repeat("━", filled) + muted + strings.Repeat("─", length-filled) + reset
	return prefix + bar + usageColor(used) + fmt.Sprintf("  %4s  ", percent) + reset + muted + resetLabel(item.Window.Resets, now) + reset
}
func (p *Panel) accountLines(a *App, names []string, s Settings, now time.Time) ([]string, int) {
	lines := []string{}
	chosen := 0
	global := a.selected()
	active := global
	if effective, err := a.accountForDirectory(currentDirectory()); err == nil {
		active = effective
	}
	for index, name := range names {
		record, ok := p.Records[name]
		state := quotaRecordState(record)
		if index == p.Cursor {
			chosen = len(lines)
		}
		email := clean(record.Account.Email)
		if email == "" {
			email = name
		}
		label := email
		if custom := strings.TrimSpace(s.DisplayNames[name]); custom != "" {
			label = clean(custom)
		}
		title := bold + fmt.Sprintf("  %d  %s", index+1, label) + reset
		if record.Account.Email != "" {
			meta := name
			if label == name || label == email {
				meta = name + " · " + clean(record.Account.Plan)
			} else if record.Account.Plan != "" {
				meta = name + " · " + clean(record.Account.Plan)
			}
			title += muted + "  [" + meta + "]" + reset
		}
		if name == active {
			title += accent(p.Theme) + "   ● active" + reset
		} else if name == global {
			title += muted + "   (global default)" + reset
		}
		if disabled(s, name) {
			title += muted + "   (disabled)" + reset
		}
		detail := p.Mode != "home" || name == active
		if !detail && ok && state == quotaValid {
			for _, item := range quotaWindows(record.Limits) {
				if (item.Label == "5h" || item.Label == "7d") && item.Window.Used != nil {
					title += usageColor(item.Window.Used) + fmt.Sprintf("   %s %g%%", item.Label, *item.Window.Used) + reset
				}
			}
		}
		if (p.Mode == "switch" || p.Mode == "project-switch" || p.Mode == "rename" || p.Mode == "disable" || p.Mode == "remove") && index == p.Cursor {
			title = highlight + " ▌" + strings.TrimPrefix(stripANSI(title), "  ") + reset
		}
		lines = append(lines, title)
		if detail && (state == quotaValid || state == quotaStale) {
			items := quotaWindows(record.Limits)
			for _, item := range items {
				lines = append(lines, quotaLine(item, p.Width, now))
			}
			if state == quotaStale {
				lines = append(lines, "     \x1b[33mQuota data stale — "+clean(record.Error)+reset)
				lines = append(lines, muted+"     Last valid reading: "+time.Unix(record.Updated, 0).Format("15:04:05")+reset)
			} else {
				lines = append(lines, muted+"     Quota data valid · updated "+time.Unix(record.Updated, 0).Format("15:04:05")+reset)
			}
		} else if ok && state == quotaUnavailable {
			lines = append(lines, "     \x1b[31mQuota data unavailable"+reset)
			if record.Error != "" {
				lines = append(lines, muted+"     "+clean(record.Error)+reset)
			}
		} else if !ok {
			lines = append(lines, muted+"     Waiting for quota query…"+reset)
		}
		lines = append(lines, "")
	}
	return lines, chosen
}
func (p *Panel) autoLines(a *App, s Settings, now time.Time) []string {
	status := "off"
	if s.Auto.Enabled {
		status = "on"
	}
	running := "stopped"
	if s.Auto.Enabled && a.daemonRunning() {
		running = "running"
	}
	lines := []string{accent(p.Theme) + bold + fmt.Sprintf("  Auto-switch %s  ·  monitor %s", status, running) + reset, muted + fmt.Sprintf("  Threshold %d%%  ·  refresh %ds  ·  cooldown %ds", s.Auto.Threshold, s.Auto.Interval, s.Auto.Cooldown) + reset, muted + "  Uses 5h / 7d Codex quotas. Disabled accounts are excluded." + reset, muted + "  Changes NEW Codex processes; running processes keep their account." + reset, ""}
	previous := a.autoState()
	message := previous.Message
	if message == "" {
		message = "No automatic check yet. Press e to enable the monitor."
	}
	lines = append(lines, "  "+clean(message))
	if previous.Checked > 0 {
		lines = append(lines, muted+"  Last check: "+time.Unix(previous.Checked, 0).Format("Jan 02 15:04:05")+reset)
	}
	lines = append(lines, "")
	for _, name := range a.names() {
		record, ok := p.Records[name]
		if !ok {
			record = previous.Records[name]
		}
		note := "quotas unavailable"
		if quotaRecordState(record) == quotaStale {
			note = "quotas stale — excluded from rotation"
		} else if score, valid := quotaScore(record, now.Unix(), max(120, s.Auto.Interval*2)); valid {
			note = fmt.Sprintf("%g%% used · %g%% remaining", score, max(0, 100-score))
		}
		if disabled(s, name) {
			note = "disabled — excluded from rotation"
		}
		if name == a.selected() {
			note += " · active"
		}
		lines = append(lines, fmt.Sprintf("  %-16s %s", name, note))
	}
	if len(previous.History) > 0 {
		lines = append(lines, "", muted+"  Recent switches"+reset)
		start := max(0, len(previous.History)-4)
		for _, event := range previous.History[start:] {
			lines = append(lines, muted+fmt.Sprintf("  %s  %s → %s", time.Unix(event.At, 0).Format("15:04:05"), event.From, event.To)+reset)
		}
	}
	return lines
}

var ansiSequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func stripANSI(s string) string { return ansiSequence.ReplaceAllString(s, "") }
func fitANSI(s string, width int) string {
	var b strings.Builder
	cells := 0
	escape := false
	for _, r := range s {
		if r == 27 {
			escape = true
		}
		if !escape {
			if cells >= width {
				break
			}
			cells++
		}
		b.WriteRune(r)
		if escape && r == 'm' {
			escape = false
		}
	}
	return b.String() + reset
}
func (p *Panel) render(a *App, names []string, s Settings, now time.Time) string {
	rows := make([]string, p.Height)
	if p.Height < 20 || p.Width < 50 {
		rows[0] = "Resize the terminal to at least 50×20. Press q to quit."
		return renderRows(rows, p.Width)
	}
	heading := map[string]string{"home": "xswap", "watch": "watching all accounts", "auto": "auto-switch view", "switch": "select account", "project-select": "select project", "session-select": "select sessions to continue", "unmanaged-warning": "restart open sessions", "project-switch": "select destination account", "rename": "select account to rename", "rename-input": "rename account", "disable": "disable / enable account", "remove": "remove account", "confirm": "confirm removal", "confirm-update": "confirm update", "confirm-project-switch": "review session continuation"}[p.Mode]
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
		rows[1] = muted + "  from " + clean(a.displayName(p.HandoffSource)) + "  ·  project " + clean(filepath.Base(p.HandoffProject)) + reset
	}
	lines, chosen := p.accountLines(a, names, s, now)
	if p.Mode == "project-select" {
		lines, chosen = p.projectLines()
	}
	if p.Mode == "session-select" {
		lines, chosen = p.sessionLines()
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
		scope := muted + "  Other projects and unselected sessions are not changed." + reset
		if isHomeScope(p.HandoffProject) {
			scope = "\x1b[33m  Global account: new Codex processes in unpinned directories will use this account." + reset
		}
		lines = []string{"", bold + "  Continue sessions from " + clean(a.displayName(p.HandoffSource)) + " with " + clean(a.displayName(p.Pending)) + "?" + reset,
			muted + "  Scope: " + clean(filepath.Base(p.HandoffProject)) + reset,
			muted + fmt.Sprintf("  Selected conversations: %d", len(selected)) + reset}
		for _, session := range selected {
			lines = append(lines, "    • "+sessionTitle(session, p.HandoffProject))
		}
		lines = append(lines,
			muted+fmt.Sprintf("  Restart and resume %d currently managed Codex session(s).", p.HandoffManaged)+reset,
			scope, "", accent(p.Theme)+"  enter / y Confirm continuation   esc Back"+reset)
	}
	if p.Mode == "unmanaged-warning" {
		lines = []string{"", bold + "  These open sessions are not managed by XSwap:" + reset}
		for _, session := range p.selectedUnmanagedSessions() {
			lines = append(lines, "    • "+sessionTitle(session, p.HandoffProject))
		}
		lines = append(lines, "", "\x1b[33m  Close each session and reopen it with codex resume in this project."+reset,
			muted+"  Then open XSwap and select the sessions again. No conversation was copied."+reset,
			"", accent(p.Theme)+"  enter / esc Back"+reset)
	}
	if p.Mode == "rename-input" {
		lines = append(lines, "", bold+"  Display name: "+p.Input+"█"+reset, muted+"  Type a name, or leave it empty to use the e-mail. Enter saves; Esc cancels."+reset)
	}
	available := p.Height - 6
	if p.Mode == "home" {
		menuItems := p.menu()
		available = max(1, p.Height-len(menuItems)-11)
	}
	if p.Mode == "switch" || p.Mode == "project-select" || p.Mode == "session-select" || p.Mode == "project-switch" || p.Mode == "disable" || p.Mode == "remove" {
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
	if p.Mode == "project-select" {
		footer = "  enter Select project   esc Back   q Quit"
	}
	if p.Mode == "confirm-project-switch" {
		footer = "  enter / y Confirm continuation   esc Back   q Quit"
	}
	if p.Mode == "unmanaged-warning" {
		footer = "  enter / esc Back   q Quit"
	}
	rows[p.Height-2] = accent(p.Theme) + footer + reset
	rows[p.Height-1] = muted + "  ↑/↓ Navigate  ·  Enter Select  ·  Percentages show quota usage" + reset
	return renderRows(rows, p.Width)
}
func renderRows(rows []string, width int) string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, row := range rows {
		b.WriteString("\x1b[2K")
		b.WriteString(fitANSI(row, width-1))
		if i < len(rows)-1 {
			b.WriteString("\r\n")
		}
	}
	return b.String()
}
func (p *Panel) key(a *App, names []string, key string) (string, error) {
	if key == "q" || key == "ctrl-c" {
		return "quit", nil
	}
	if key == "esc" {
		if p.Mode == "home" {
			return "quit", nil
		}
		if p.Mode == "rename-input" {
			p.Mode = "rename"
			p.Input = ""
			return "", nil
		}
		if p.Mode == "confirm-project-switch" {
			p.Mode = "project-switch"
			p.Offset = 0
			return "", nil
		}
		if p.Mode == "session-select" && len(p.HandoffProjects) > 0 {
			p.Mode = "project-select"
			p.Offset = 0
			p.Cursor = 0
			for index, project := range p.HandoffProjects {
				if filepath.Clean(project.Root) == filepath.Clean(p.HandoffProject) {
					p.Cursor = index
					break
				}
			}
			return "", nil
		}
		if p.Mode == "project-switch" {
			p.Mode = "session-select"
			p.Offset = 0
			p.Cursor = 0
			return "", nil
		}
		if p.Mode == "unmanaged-warning" {
			p.Mode = "session-select"
			p.Offset = 0
			return "", nil
		}
		p.Mode = "home"
		p.Offset = 0
		p.Pending = ""
		return "", nil
	}
	if p.Mode == "confirm" {
		if key == "y" || key == "Y" {
			return "remove:" + p.Pending, nil
		}
		return "", nil
	}
	if p.Mode == "confirm-update" {
		if key == "y" || key == "Y" {
			return "update", nil
		}
		return "", nil
	}
	if p.Mode == "confirm-project-switch" {
		if key == "enter" || key == "y" || key == "Y" {
			return handoffAction(p.HandoffProject, p.Pending, p.selectedSessions()), nil
		}
		return "", nil
	}
	if p.Mode == "unmanaged-warning" {
		if key == "enter" {
			p.Mode = "session-select"
			p.Offset = 0
		}
		return "", nil
	}
	if p.Mode == "rename-input" {
		if key == "backspace" || key == "delete" {
			if len(p.Input) > 0 {
				p.Input = p.Input[:len(p.Input)-1]
			}
			return "", nil
		}
		if key == "enter" {
			if err := a.setDisplayName(names[p.Cursor], p.Input); err != nil {
				p.Message = err.Error()
			} else {
				p.Message = "Display name updated for " + names[p.Cursor]
				p.Mode = "home"
				p.Input = ""
				p.Offset = 0
			}
			return "", nil
		}
		if key != "up" && key != "down" && len([]rune(key)) == 1 && key >= " " && key != "\x7f" {
			if len([]rune(p.Input)) < 64 {
				p.Input += key
			}
			return "", nil
		}
	}
	if p.Mode == "session-select" && (key == "a" || key == "A") {
		selectAll := len(p.selectedSessions()) != len(p.HandoffSessions)
		for _, session := range p.HandoffSessions {
			p.HandoffSelected[session.ID] = selectAll
		}
		return "", nil
	}
	if key == "t" || key == "ctrl-t" {
		p.Theme = (p.Theme + 1) % 3
		return "", nil
	}
	switch key {
	case " ":
		if p.Mode == "session-select" && len(p.HandoffSessions) > 0 {
			id := p.HandoffSessions[p.Cursor].ID
			p.HandoffSelected[id] = !p.HandoffSelected[id]
		}
	case "s":
		p.Mode = "switch"
		p.Offset = 0
	case "w":
		p.Mode = "watch"
		p.Offset = 0
	case "u":
		p.Mode = "auto"
		p.Offset = 0
	case "a":
		return "add", nil
	case "r":
		return "refresh", nil
	case "e":
		if p.Mode == "auto" {
			s, err := a.settings()
			if err != nil {
				return "", err
			}
			if err = a.configureAuto(!s.Auto.Enabled, 0, 0); err != nil {
				return "", err
			}
			p.Message = "Auto-switch changes new Codex processes only."
		}
	case "+", "-":
		if p.Mode == "auto" {
			s, err := a.settings()
			if err != nil {
				return "", err
			}
			delta := 5
			if key == "-" {
				delta = -5
			}
			return "", a.configureAuto(s.Auto.Enabled, max(1, min(100, s.Auto.Threshold+delta)), 0)
		}
	case "down", "j":
		if p.Mode == "home" {
			p.MenuCursor = (p.MenuCursor + 1) % len(p.menu())
		} else if p.Mode == "project-select" {
			p.Cursor = min(p.Cursor+1, len(p.HandoffProjects)-1)
		} else if p.Mode == "session-select" {
			p.Cursor = min(p.Cursor+1, len(p.HandoffSessions)-1)
		} else if p.Mode == "switch" || p.Mode == "project-switch" || p.Mode == "rename" || p.Mode == "disable" || p.Mode == "remove" {
			p.Cursor = min(p.Cursor+1, len(names)-1)
		} else {
			p.Offset++
		}
	case "up", "k":
		if p.Mode == "home" {
			p.MenuCursor = (p.MenuCursor + len(p.menu()) - 1) % len(p.menu())
		} else if p.Mode == "project-select" {
			p.Cursor = max(0, p.Cursor-1)
		} else if p.Mode == "session-select" {
			p.Cursor = max(0, p.Cursor-1)
		} else if p.Mode == "switch" || p.Mode == "project-switch" || p.Mode == "rename" || p.Mode == "disable" || p.Mode == "remove" {
			p.Cursor = max(0, p.Cursor-1)
		} else {
			p.Offset = max(0, p.Offset-1)
		}
	case "enter":
		if p.Mode == "home" {
			if p.menu()[p.MenuCursor] == "Update version…" {
				p.Mode = "confirm-update"
				p.Offset = 0
				return "", nil
			}
			if p.menu()[p.MenuCursor] == "Theme…" {
				p.Theme = (p.Theme + 1) % 3
				return "", nil
			}
			if p.menu()[p.MenuCursor] == "Quit" {
				return "quit", nil
			}
			switch p.menu()[p.MenuCursor] {
			case "Switch account…":
				p.Mode = "switch"
			case "Continue sessions with another account…":
				root, rootErr := projectRoot(currentDirectory())
				if rootErr != nil {
					p.Message = rootErr.Error()
					return "", nil
				}
				if isHomeScope(root) {
					projects, projectsErr := a.sessionProjects()
					if projectsErr != nil {
						p.Message = projectsErr.Error()
						return "", nil
					}
					if len(projects) == 0 {
						p.Message = "No projects with Codex conversations were found in any XSwap account."
						return "", nil
					}
					p.HandoffProjects = projects
					p.Mode = "project-select"
				} else {
					p.HandoffProjects = nil
					if openErr := p.openHandoffProject(a, root); openErr != nil {
						p.Message = openErr.Error()
						return "", nil
					}
				}
			case "Watch accounts":
				p.Mode = "watch"
			case "Auto-switch view":
				p.Mode = "auto"
			case "Add account…":
				return "add", nil
			case "Rename account…":
				p.Mode = "rename"
				p.Input = ""
			case "Disable / enable account…":
				p.Mode = "disable"
			case "Remove account…":
				p.Mode = "remove"
			case "Theme…":
				p.Theme = (p.Theme + 1) % 3
			case "Quit":
				return "quit", nil
			}
			p.Offset = 0
			p.Cursor = 0
		} else if p.Mode == "project-select" {
			if len(p.HandoffProjects) > 0 {
				if err := p.openHandoffProject(a, p.HandoffProjects[p.Cursor].Root); err != nil {
					p.Message = err.Error()
				}
			}
		} else if p.Mode == "session-select" {
			if len(p.selectedSessions()) == 0 {
				p.Message = "Select at least one conversation to continue."
			} else if len(p.selectedUnmanagedSessions()) > 0 {
				p.Mode = "unmanaged-warning"
				p.Offset = 0
			} else if len(names) < 2 {
				p.Message = "Add another account before continuing these conversations."
			} else {
				p.Mode = "project-switch"
				p.Cursor = 0
				for index, name := range names {
					if name != p.HandoffSource {
						p.Cursor = index
						break
					}
				}
				p.Offset = 0
			}
		} else if p.Mode == "switch" {
			if err := a.selectAccount(names[p.Cursor]); err != nil {
				p.Message = err.Error()
			} else {
				p.Message = "Selected " + names[p.Cursor] + " for new Codex processes."
				if os.Getenv("CODEX_HOME") != "" {
					p.Message = "Explicit CODEX_HOME takes priority; unset it to apply the selection."
				} else if effective, accountErr := a.accountForDirectory(currentDirectory()); accountErr == nil && effective != names[p.Cursor] {
					p.Message = "Global default changed to " + names[p.Cursor] + "; this project remains on " + effective + "."
				}
				p.Mode = "home"
				p.Offset = 0
			}
		} else if p.Mode == "project-switch" {
			plan, err := a.planSelectedProjectHandoff(p.HandoffProject, names[p.Cursor], p.HandoffSelected)
			if err != nil {
				p.Message = err.Error()
			} else if len(plan.Unmanaged) > 0 {
				p.HandoffUnmanaged = map[string]bool{}
				for _, session := range plan.Unmanaged {
					p.HandoffUnmanaged[session.ID] = true
				}
				p.Mode = "unmanaged-warning"
				p.Offset = 0
			} else {
				p.Pending = names[p.Cursor]
				p.HandoffProject = plan.Project
				p.HandoffSource = plan.Source
				p.HandoffManaged = len(plan.Managed)
				p.Mode = "confirm-project-switch"
				p.Offset = 0
			}
		} else if p.Mode == "rename" {
			name := names[p.Cursor]
			p.Pending = name
			p.Input = ""
			if s, err := a.settings(); err == nil {
				p.Input = s.DisplayNames[name]
			}
			p.Mode = "rename-input"
		} else if p.Mode == "disable" {
			s, err := a.settings()
			if err != nil {
				return "", err
			}
			name := names[p.Cursor]
			if err = a.setEnabled(name, disabled(s, name)); err != nil {
				p.Message = err.Error()
			} else {
				p.Message = "Updated rotation eligibility for " + name
			}
		} else if p.Mode == "remove" {
			name := names[p.Cursor]
			if name == "default" {
				p.Message = "default is protected; disable it to exclude it from rotation."
			} else if name == a.selected() {
				p.Message = "Switch to another account before removing the active account."
			} else {
				p.Pending = name
				p.Mode = "confirm"
				p.Offset = 0
			}
		}
	}
	return "", nil
}
func parseKeys(buffer []byte, flush bool) ([]string, []byte) {
	keys := []string{}
	for len(buffer) > 0 {
		if buffer[0] == 27 {
			if len(buffer) >= 3 && (buffer[1] == '[' || buffer[1] == 'O') {
				switch buffer[2] {
				case 'A':
					keys = append(keys, "up")
				case 'B':
					keys = append(keys, "down")
				}
				buffer = buffer[3:]
				continue
			}
			if len(buffer) < 3 && !flush {
				return keys, buffer
			}
			keys = append(keys, "esc")
			buffer = buffer[1:]
			continue
		}
		key := string(buffer[:1])
		switch buffer[0] {
		case 3:
			key = "ctrl-c"
		case 127:
			key = "backspace"
		case 20:
			key = "ctrl-t"
		case 10, 13:
			key = "enter"
		}
		keys = append(keys, key)
		buffer = buffer[1:]
	}
	return keys, buffer
}
func (a *App) panel(mode, filter string, interval int) (string, error) {
	saved, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	fmt.Print("\x1b[?1049h\x1b[?25l\x1b[2J")
	defer func() {
		_ = term.Restore(int(os.Stdin.Fd()), saved)
		fmt.Print(reset + "\x1b[?25h\x1b[?1049l")
	}()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	updates := make(chan panelUpdate, 32)
	input := make(chan []byte, 16)
	readNext := make(chan struct{}, 1)
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		buffer := make([]byte, 64)
		for ctx.Err() == nil {
			n, err := readStdin(buffer)
			if err != nil {
				return
			}
			if n == 0 {
				continue
			}
			data := append([]byte{}, buffer[:n]...)
			select {
			case input <- data:
			case <-ctx.Done():
				return
			}
			// Do not read ahead: the next action may hand stdin to login or a prompt.
			select {
			case <-readNext:
			case <-ctx.Done():
				return
			}
		}
	}()
	h, w := terminalSize()
	p := Panel{Mode: mode, Filter: filter, Records: map[string]Record{}, Width: w, Height: h}
	updateChecks := make(chan bool, 1)
	checkDue := time.Now().Add(updateCheckInterval)
	check := func() {
		go func() {
			available := a.checkUpdate(ctx)
			select {
			case updateChecks <- available:
			case <-ctx.Done():
			}
		}()
	}
	check()
	workerDone := make(chan struct{})
	close(workerDone)
	fetchCancel := func() {}
	refresh := func() {
		if p.Busy {
			return
		}
		p.Busy = true
		fetchCtx, stop := context.WithCancel(ctx)
		fetchCancel = stop
		workerDone = make(chan struct{})
		names := a.names()
		if filter != "" {
			names = []string{filter}
		}
		go func(done chan struct{}) {
			defer close(done)
			defer stop()
			for _, name := range names {
				if fetchCtx.Err() != nil {
					return
				}
				record, err := a.readLimits(fetchCtx, name)
				message := ""
				if err != nil {
					message = err.Error()
				}
				select {
				case updates <- panelUpdate{Name: name, Record: record, Error: message}:
				case <-fetchCtx.Done():
					return
				}
			}
			select {
			case updates <- panelUpdate{Done: true}:
			case <-fetchCtx.Done():
			}
		}(workerDone)
	}
	defer func() {
		cancel()
		fetchCancel()
		select {
		case <-workerDone:
		case <-time.After(3 * time.Second):
		}
		select {
		case <-inputDone:
		case <-time.After(200 * time.Millisecond):
		}
	}()
	a.ensureDaemon()
	refresh()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	lastDraw := time.Time{}
	var buffer []byte
	received := time.Time{}
	for {
		dirty := false
		inputReceived := false
		var keys []string
		select {
		case <-ctx.Done():
			return "", nil
		case data := <-input:
			inputReceived = true
			buffer = append(buffer, data...)
			received = time.Now()
			keys, buffer = parseKeys(buffer, false)
			dirty = true
		case update := <-updates:
			if update.Done {
				p.Busy = false
				p.Due = time.Now().Add(time.Duration(interval) * time.Second)
			} else {
				p.Records[update.Name] = reconcileQuotaRecord(p.Records[update.Name], update.Record, update.Error)
			}
			dirty = true
		case available := <-updateChecks:
			if p.UpdateAvailable != available {
				p.MenuCursor = 0
			}
			p.UpdateAvailable = available
			dirty = true
		case <-tick.C:
			if !time.Now().Before(checkDue) {
				check()
				checkDue = time.Now().Add(updateCheckInterval)
			}
			if len(buffer) > 0 && time.Since(received) > 120*time.Millisecond {
				keys, buffer = parseKeys(buffer, true)
				dirty = true
			}
		}
		if !p.Busy && !p.Due.IsZero() && !time.Now().Before(p.Due) {
			refresh()
		}
		names := a.names()
		if filter != "" {
			names = []string{filter}
		}
		if p.Mode == "project-select" {
			p.Cursor = min(p.Cursor, len(p.HandoffProjects)-1)
		} else if p.Mode == "session-select" {
			p.Cursor = min(p.Cursor, len(p.HandoffSessions)-1)
		} else {
			p.Cursor = min(p.Cursor, len(names)-1)
		}
		for _, key := range keys {
			if p.Mode == "confirm" && (key == "y" || key == "Y") {
				fetchCancel()
				select {
				case <-workerDone:
				case <-time.After(3 * time.Second):
				}
				p.Busy = false
			}
			action, err := p.key(a, names, key)
			if err != nil {
				p.Message = err.Error()
			}
			if action == "quit" || action == "add" || action == "update" || strings.HasPrefix(action, "remove:") || strings.HasPrefix(action, "project-handoff:") {
				return action, nil
			}
			if action == "refresh" {
				refresh()
			}
		}
		if inputReceived {
			readNext <- struct{}{}
		}
		if dirty || time.Since(lastDraw) >= time.Second {
			h, w = terminalSize()
			p.Width = w
			p.Height = h
			s, err := a.settings()
			if err != nil {
				return "", err
			}
			fmt.Print(p.render(a, names, s, time.Now()))
			lastDraw = time.Now()
		}
	}
}
