package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
func (a *App) dashboard(mode, filter string, interval int) error {
	if !interactive() {
		return errors.New("the panel needs an interactive terminal; use xswap watch --once or xswap limits")
	}
	action, err := a.panel(mode, filter, interval)
	if err != nil {
		return err
	}
	if action == "restart" {
		return replaceProcess(a.Binary, nil, os.Environ())
	}
	return nil
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
	HandoffArchives                       int
	HandoffProject                        string
	HandoffSources                        []string
	HandoffProjects                       []sessionProject
	HandoffSessions                       []codexSession
	HandoffSelected, HandoffRunning       map[string]bool
	HandoffAwaiting                       map[string]bool
	HandoffUnmanaged                      map[string]bool
	HandoffConflicts                      map[string][]codexSession
	HandoffResolutions                    map[string]string
	HandoffConflictID                     string
}

func handoffAction(project, target string, sessions []codexSession, resolutions map[string]string) string {
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, session.ID)
	}
	encodedProject := base64.RawURLEncoding.EncodeToString([]byte(project))
	encodedResolutions := ""
	if len(resolutions) > 0 {
		data, _ := json.Marshal(resolutions)
		encodedResolutions = base64.RawURLEncoding.EncodeToString(data)
	}
	return "project-handoff:" + encodedProject + ":" + target + ":" + strings.Join(ids, ",") + ":" + encodedResolutions
}

func parseHandoffAction(action string) (string, string, map[string]bool, map[string]string, error) {
	parts := strings.SplitN(strings.TrimPrefix(action, "project-handoff:"), ":", 4)
	if len(parts) < 3 {
		return "", "", nil, nil, errors.New("session handoff action is invalid")
	}
	projectBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || !filepath.IsAbs(string(projectBytes)) {
		return "", "", nil, nil, errors.New("session handoff project is invalid")
	}
	selected := map[string]bool{}
	for _, id := range strings.Split(parts[2], ",") {
		if sessionIDPattern.MatchString(id) {
			selected[id] = true
		}
	}
	resolutions := map[string]string{}
	if len(parts) == 4 && parts[3] != "" {
		data, decodeErr := base64.RawURLEncoding.DecodeString(parts[3])
		if decodeErr != nil || json.Unmarshal(data, &resolutions) != nil {
			return "", "", nil, nil, errors.New("session handoff resolutions are invalid")
		}
		for id, account := range resolutions {
			if !sessionIDPattern.MatchString(id) || (account != "default" && validate(account) != nil) {
				return "", "", nil, nil, errors.New("session handoff resolution is invalid")
			}
		}
	}
	return filepath.Clean(string(projectBytes)), parts[1], selected, resolutions, nil
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

func (p *Panel) nextUnresolvedConflict() string {
	for _, session := range p.HandoffSessions {
		if p.HandoffSelected[session.ID] && len(p.HandoffConflicts[session.ID]) > 0 && p.HandoffResolutions[session.ID] == "" {
			return session.ID
		}
	}
	return ""
}

func (p *Panel) beginConflictResolution() bool {
	id := p.nextUnresolvedConflict()
	if id == "" {
		return false
	}
	p.HandoffConflictID = id
	p.Mode = "conflict-source"
	p.Cursor = 0
	p.Offset = 0
	p.Message = ""
	return true
}

func (p *Panel) selectedSourceAccounts() []string {
	found := map[string]bool{}
	for _, session := range p.selectedSessions() {
		account := session.Account
		if resolved := p.HandoffResolutions[session.ID]; resolved != "" {
			account = resolved
		}
		if account != "" {
			found[account] = true
		}
	}
	accounts := make([]string, 0, len(found))
	for account := range found {
		accounts = append(accounts, account)
	}
	sort.Strings(accounts)
	return accounts
}

func (p *Panel) conflictSourceLines(a *App) ([]string, int) {
	copies := p.HandoffConflicts[p.HandoffConflictID]
	lines := []string{"", bold + "  Choose the source history to keep" + reset, muted + "  Other copies remain untouched unless the destination copy must be archived." + reset, ""}
	chosen := len(lines)
	for index, session := range copies {
		if index == p.Cursor {
			chosen = len(lines)
		}
		label := fmt.Sprintf("  %d  %s", index+1, clean(a.displayName(session.Account)))
		if index == p.Cursor {
			label = highlight + accent(p.Theme) + " ▌ " + strings.TrimSpace(label) + reset
		}
		lines = append(lines, label, muted+"     updated "+session.Updated.Format("Jan 02 15:04")+reset, "")
	}
	return lines, chosen
}

func (p *Panel) useHandoffPlan(plan projectHandoffPlan) {
	p.HandoffProject = plan.Project
	p.HandoffSources = plan.Sources
	p.HandoffSessions = plan.Sessions
	p.HandoffSelected = map[string]bool{}
	p.HandoffRunning = map[string]bool{}
	p.HandoffAwaiting = map[string]bool{}
	p.HandoffUnmanaged = map[string]bool{}
	p.HandoffConflicts = map[string][]codexSession{}
	p.HandoffResolutions = map[string]string{}
	p.HandoffConflictID = ""
	p.HandoffArchives = 0
	for _, session := range plan.Sessions {
		p.HandoffSelected[session.ID] = true
	}
	for _, record := range plan.Managed {
		if record.SessionID != "" {
			p.HandoffRunning[record.SessionID] = true
		}
	}
	for _, session := range plan.Awaiting {
		p.HandoffAwaiting[session.ID] = true
	}
	for _, session := range plan.Unmanaged {
		p.HandoffUnmanaged[session.ID] = true
	}
	for _, conflict := range plan.Conflicts {
		p.HandoffConflicts[conflict.ID] = append([]codexSession(nil), conflict.Copies...)
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
		accounts := fmt.Sprintf("across %d accounts", project.Accounts)
		if project.Accounts == 1 {
			accounts = "in 1 account"
		}
		meta := fmt.Sprintf("     %s  ·  %d %s %s  ·  last used %s", clean(projectLabel(project.Root)), project.Count, conversation, accounts, project.Updated.Format("Jan 02 15:04"))
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

func (p *Panel) sessionLines(a *App) ([]string, int) {
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
		meta := session.Updated.Format("Jan 02 15:04") + "  ·  from " + a.displayName(session.Account)
		if p.HandoffRunning[session.ID] {
			meta += "  ·  running"
		} else if p.HandoffAwaiting[session.ID] {
			meta += "  ·  supervised · awaiting identification"
		} else if p.HandoffUnmanaged[session.ID] {
			meta += "  ·  open outside XSwap"
		}
		if copies := p.HandoffConflicts[session.ID]; len(copies) > 0 {
			accounts := make([]string, 0, len(copies))
			for _, copy := range copies {
				accounts = append(accounts, a.displayName(copy.Account))
			}
			if source := p.HandoffResolutions[session.ID]; source != "" {
				meta += "  ·  diverged · keep " + a.displayName(source)
			} else {
				meta += "  ·  diverged across " + strings.Join(accounts, ", ") + " · source required"
			}
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
