package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
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
func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	data, err := cmd.Output()
	return strings.TrimSpace(string(data)), err
}
func interactive() bool {
	if os.Getenv("TERM") == "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	_, err := stty("-g")
	return err == nil
}
func terminalSize() (int, int) {
	data, err := stty("size")
	if err == nil {
		fields := strings.Fields(data)
		if len(fields) == 2 {
			h, _ := strconv.Atoi(fields[0])
			w, _ := strconv.Atoi(fields[1])
			if h > 0 && w > 0 {
				return h, w
			}
		}
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
			installed, updateErr := a.updateCommand(Options{})
			if updateErr != nil {
				fmt.Println("Update failed:", updateErr)
			}
			if installed {
				return syscall.Exec(a.Binary, []string{a.Binary}, os.Environ())
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
}

var menuItems = []string{"Switch account…", "Watch accounts", "Auto-switch view", "Add account…", "Rename account…", "Disable / enable account…", "Remove account…", "Theme…", "Quit"}

func (p *Panel) menu() []string {
	items := append([]string{}, menuItems[:6]...)
	if p.UpdateAvailable {
		items = append(items, "Update version…")
	}
	return append(items, menuItems[6:]...)
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
	active := a.selected()
	for index, name := range names {
		record, ok := p.Records[name]
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
		}
		if disabled(s, name) {
			title += muted + "   (disabled)" + reset
		}
		detail := p.Mode != "home" || name == active
		if !detail && ok {
			for _, item := range quotaWindows(record.Limits) {
				if (item.Label == "5h" || item.Label == "7d") && item.Window.Used != nil {
					title += usageColor(item.Window.Used) + fmt.Sprintf("   %s %g%%", item.Label, *item.Window.Used) + reset
				}
			}
		}
		if (p.Mode == "switch" || p.Mode == "rename" || p.Mode == "disable" || p.Mode == "remove") && index == p.Cursor {
			title = highlight + " ▌" + strings.TrimPrefix(stripANSI(title), "  ") + reset
		}
		lines = append(lines, title)
		if record.Error != "" {
			lines = append(lines, "     \x1b[31m"+clean(record.Error)+reset)
		}
		if detail && record.Updated > 0 && record.Account.Type != "" {
			items := quotaWindows(record.Limits)
			if len(items) == 0 {
				lines = append(lines, muted+"     No quota windows returned by the server."+reset)
			}
			for _, item := range items {
				lines = append(lines, quotaLine(item, p.Width, now))
			}
			if record.Error != "" {
				lines = append(lines, muted+"     Last successful query: "+time.Unix(record.Updated, 0).Format("15:04:05")+reset)
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
		if score, valid := quotaScore(record, now.Unix(), max(120, s.Auto.Interval*2)); valid {
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
	heading := map[string]string{"home": "xswap", "watch": "watching all accounts", "auto": "auto-switch view", "switch": "select account", "rename": "rename account", "disable": "disable / enable account", "remove": "remove account", "confirm": "confirm removal"}[p.Mode]
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
	rows[0] = muted + "  " + heading + "  ·  " + status + reset
	lines, chosen := p.accountLines(a, names, s, now)
	if p.Mode == "auto" {
		lines = p.autoLines(a, s, now)
	}
	if p.Mode == "confirm" {
		lines = []string{"", bold + "  Remove " + p.Pending + " from the account list?" + reset, muted + "  Credentials and history will be archived locally, not deleted." + reset, "", accent(p.Theme) + "  y Confirm removal   esc Cancel" + reset}
	}
	if p.Mode == "rename" {
		lines = append(lines, "", bold+"  Display name: "+p.Input+"█"+reset, muted+"  Type a name, or leave it empty to use the e-mail. Enter saves; Esc cancels."+reset)
	}
	available := p.Height - 6
	if p.Mode == "home" {
		menuItems := p.menu()
		available = max(1, p.Height-len(menuItems)-11)
	}
	if p.Mode == "switch" || p.Mode == "disable" || p.Mode == "remove" {
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
	if p.Mode == "rename" {
		footer = "  Type display name   enter Save   backspace Delete   esc Cancel   q Quit"
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
		p.Mode = "home"
		p.Offset = 0
		p.Pending = ""
		return "", nil
	}
	if p.Mode == "confirm" {
		if key == "y" {
			archive, err := a.remove(p.Pending)
			if err != nil {
				p.Message = err.Error()
			} else {
				delete(p.Records, p.Pending)
				p.Message = "Account removed. Data archived at " + archive
			}
			p.Pending = ""
			p.Mode = "home"
			p.Cursor = 0
			p.Offset = 0
		}
		return "", nil
	}
	if p.Mode == "rename" {
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
		if key == "up" || key == "down" || key == "j" || key == "k" { /* handled below */
		} else if len([]rune(key)) == 1 && key >= " " && key != "\x7f" {
			if len([]rune(p.Input)) < 64 {
				p.Input += key
			}
			return "", nil
		}
	}
	if key == "t" || key == "ctrl-t" {
		p.Theme = (p.Theme + 1) % 3
		return "", nil
	}
	switch key {
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
		} else if p.Mode == "switch" || p.Mode == "rename" || p.Mode == "disable" || p.Mode == "remove" {
			p.Cursor = min(p.Cursor+1, len(names)-1)
		} else {
			p.Offset++
		}
	case "up", "k":
		if p.Mode == "home" {
			p.MenuCursor = (p.MenuCursor + len(p.menu()) - 1) % len(p.menu())
		} else if p.Mode == "switch" || p.Mode == "rename" || p.Mode == "disable" || p.Mode == "remove" {
			p.Cursor = max(0, p.Cursor-1)
		} else {
			p.Offset = max(0, p.Offset-1)
		}
	case "enter":
		if p.Mode == "home" {
			if p.menu()[p.MenuCursor] == "Update version…" {
				return "update", nil
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
			case "Watch accounts":
				p.Mode = "watch"
			case "Auto-switch view":
				p.Mode = "auto"
			case "Add account…":
				return "add", nil
			case "Rename account…":
				p.Mode = "rename"
				p.Input = ""
				if s, err := a.settings(); err == nil {
					p.Input = s.DisplayNames[names[p.Cursor]]
				}
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
		} else if p.Mode == "switch" {
			if err := a.selectAccount(names[p.Cursor]); err != nil {
				p.Message = err.Error()
			} else {
				p.Message = "Selected " + names[p.Cursor] + " for new Codex processes."
				if os.Getenv("CODEX_HOME") != "" {
					p.Message = "Explicit CODEX_HOME takes priority; unset it to apply the selection."
				}
				p.Mode = "home"
				p.Offset = 0
			}
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
	saved, err := stty("-g")
	if err != nil {
		return "", err
	}
	if _, err = stty("-icanon", "-echo", "min", "0", "time", "1"); err != nil {
		return "", err
	}
	fmt.Print("\x1b[?1049h\x1b[?25l\x1b[2J")
	defer func() { stty(saved); fmt.Print(reset + "\x1b[?25h\x1b[?1049l") }()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	updates := make(chan panelUpdate, 32)
	input := make(chan []byte, 16)
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		buffer := make([]byte, 64)
		for ctx.Err() == nil {
			n, err := syscall.Read(int(os.Stdin.Fd()), buffer)
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
		}
	}()
	h, w := terminalSize()
	p := Panel{Mode: mode, Filter: filter, Records: map[string]Record{}, Width: w, Height: h}
	updateChecks := make(chan bool, 1)
	checkDue := time.Now().Add(6 * time.Hour)
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
				if err != nil {
					record = Record{Error: err.Error()}
				}
				select {
				case updates <- panelUpdate{Name: name, Record: record}:
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
		var keys []string
		select {
		case <-ctx.Done():
			return "", nil
		case data := <-input:
			buffer = append(buffer, data...)
			received = time.Now()
			keys, buffer = parseKeys(buffer, false)
			dirty = true
		case update := <-updates:
			if update.Done {
				p.Busy = false
				p.Due = time.Now().Add(time.Duration(interval) * time.Second)
			} else if update.Record.Error != "" {
				old := p.Records[update.Name]
				old.Error = update.Record.Error
				p.Records[update.Name] = old
			} else {
				p.Records[update.Name] = update.Record
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
				checkDue = time.Now().Add(6 * time.Hour)
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
		p.Cursor = min(p.Cursor, len(names)-1)
		for _, key := range keys {
			if p.Mode == "confirm" && key == "y" {
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
			if action == "quit" || action == "add" || action == "update" {
				return action, nil
			}
			if action == "refresh" {
				refresh()
			}
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
