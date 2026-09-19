package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
)

type panelTickMsg time.Time
type panelUpdateCheckMsg bool
type panelRefreshMsg struct {
	ID        uint64
	Updates   []panelUpdate
	Cancelled bool
}

type panelActionResult struct {
	Message    string
	ExitAction string
}

type panelActionDoneMsg struct {
	Result panelActionResult
	Error  error
}

type panelActionCommand struct {
	input  io.Reader
	output io.Writer
	stderr io.Writer
	run    func(io.Reader, io.Writer) panelActionResult
	result panelActionResult
}

func (c *panelActionCommand) SetStdin(input io.Reader)   { c.input = input }
func (c *panelActionCommand) SetStdout(output io.Writer) { c.output = output }
func (c *panelActionCommand) SetStderr(stderr io.Writer) { c.stderr = stderr }
func (c *panelActionCommand) Run() error {
	if c.input == nil || c.output == nil {
		return errors.New("panel action has no terminal")
	}
	c.result = c.run(c.input, c.output)
	return nil
}

type panelModel struct {
	app             *App
	panel           Panel
	names           []string
	settings        Settings
	filter          string
	interval        int
	ctx             context.Context
	action          string
	err             error
	nextUpdateCheck time.Time
	runAction       func(string, io.Reader, io.Writer) panelActionResult
	refreshCancel   context.CancelFunc
	refreshID       uint64
	pendingAction   string
}

func newPanelModel(ctx context.Context, app *App, mode, filter string, interval int) (*panelModel, error) {
	settings, err := app.settings()
	if err != nil {
		return nil, err
	}
	names := app.names()
	if filter != "" {
		names = []string{filter}
	}
	return &panelModel{
		app:       app,
		panel:     Panel{Mode: mode, Filter: filter, Records: map[string]Record{}, Width: 120, Height: 30, Busy: true},
		names:     names,
		settings:  settings,
		filter:    filter,
		interval:  interval,
		ctx:       ctx,
		runAction: app.runPanelAction,
	}, nil
}

func waitForPanelReturn(input io.Reader, output io.Writer) {
	_, _ = fmt.Fprint(output, "\nPress Enter to return to the menu.")
	_, _ = bufio.NewReader(input).ReadString('\n')
}

func (a *App) runPanelAction(action string, input io.Reader, output io.Writer) panelActionResult {
	result := panelActionResult{}
	switch {
	case action == "update":
		_, _ = fmt.Fprintln(output, "Checking for updates…")
		installed, err := a.updateCommand(Options{Flags: map[string]bool{"yes": true}})
		if err != nil {
			_, _ = fmt.Fprintln(output, "Update failed:", err)
			result.Message = "Update failed: " + err.Error()
		} else if installed {
			result.ExitAction = "restart"
			return result
		} else {
			result.Message = "XSwap is already up to date."
		}
	case action == "add":
		name, err := a.createNumbered()
		if err != nil {
			_, _ = fmt.Fprintln(output, "Account creation failed:", err)
			result.Message = "Account creation failed: " + err.Error()
			break
		}
		_, _ = fmt.Fprintf(output, "Adding %s. Sign in to the account you want to register.\n", name)
		if err = a.login(name, false); err != nil {
			_, _ = fmt.Fprintln(output, "Login incomplete:", err)
			result.Message = "Login incomplete for " + name + ": " + err.Error()
		} else {
			result.Message = "Added " + name + "."
		}
	case strings.HasPrefix(action, "remove:"):
		name := strings.TrimPrefix(action, "remove:")
		_, _ = fmt.Fprintf(output, "Removing %s and archiving its local data…\n", name)
		archive, err := a.remove(name)
		if err != nil {
			_, _ = fmt.Fprintln(output, "Removal failed:", err)
			result.Message = "Removal failed: " + err.Error()
		} else {
			_, _ = fmt.Fprintln(output, "Account removed. Data archived at", archive)
			result.Message = "Removed " + name + "."
		}
	case strings.HasPrefix(action, "project-handoff:"):
		project, target, selected, resolutions, err := parseHandoffAction(action)
		_, _ = fmt.Fprintln(output, "Switching the project account and transferring its conversations…")
		if err == nil {
			var plan projectHandoffPlan
			plan, err = a.planResolvedProjectHandoff(project, target, selected, resolutions)
			if err == nil {
				var handoff projectHandoffResult
				handoff, err = a.requestProjectHandoff(plan)
				if err == nil {
					printProjectHandoffResult(a, handoff)
					result.Message = fmt.Sprintf("Continued %d session(s) with %s.", handoff.Sessions, a.displayName(handoff.Target))
				}
			}
		}
		if err != nil {
			_, _ = fmt.Fprintln(output, "Project switch failed:", err)
			result.Message = "Project switch failed: " + err.Error()
		}
	default:
		result.Message = "Unknown panel action."
	}
	waitForPanelReturn(input, output)
	return result
}

func (m *panelModel) actionCommand(action string) tea.Cmd {
	runner := m.runAction
	if m.app.PanelActionRunner != nil {
		runner = m.app.PanelActionRunner
	}
	command := &panelActionCommand{run: func(input io.Reader, output io.Writer) panelActionResult {
		return runner(action, input, output)
	}}
	return tea.Exec(command, func(err error) tea.Msg {
		return panelActionDoneMsg{Result: command.result, Error: err}
	})
}

func panelTick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return panelTickMsg(now) })
}

func (m *panelModel) startRefresh() tea.Cmd {
	if m.refreshCancel != nil {
		m.refreshCancel()
	}
	refreshCtx, cancel := context.WithCancel(m.ctx)
	m.refreshCancel = cancel
	m.refreshID++
	id := m.refreshID
	names := append([]string{}, m.names...)
	m.panel.Busy = true
	return func() tea.Msg {
		updates := make([]panelUpdate, 0, len(names)+1)
		for _, name := range names {
			if refreshCtx.Err() != nil {
				return panelRefreshMsg{ID: id, Cancelled: true}
			}
			record, err := m.app.readLimits(refreshCtx, name)
			if refreshCtx.Err() != nil {
				return panelRefreshMsg{ID: id, Cancelled: true}
			}
			message := ""
			if err != nil {
				message = err.Error()
			}
			updates = append(updates, panelUpdate{Name: name, Record: record, Error: message})
		}
		return panelRefreshMsg{ID: id, Updates: updates}
	}
}

func (m *panelModel) updateCheckCommand() tea.Cmd {
	return func() tea.Msg { return panelUpdateCheckMsg(m.app.checkUpdate(m.ctx)) }
}

func (m *panelModel) Init() tea.Cmd {
	m.nextUpdateCheck = time.Now().Add(updateCheckInterval)
	return tea.Batch(panelTick(), m.startRefresh(), m.updateCheckCommand())
}

func bubbleKey(msg tea.KeyPressMsg) string {
	key := msg.Key()
	switch key.Code {
	case tea.KeyUp:
		return "up"
	case tea.KeyDown:
		return "down"
	case tea.KeyEnter:
		return "enter"
	case tea.KeyEsc:
		return "esc"
	case tea.KeyBackspace:
		return "backspace"
	case tea.KeyDelete:
		return "delete"
	}
	if msg.String() == "ctrl+c" || msg.Keystroke() == "ctrl+c" {
		return "ctrl-c"
	}
	if msg.String() == "ctrl+t" || msg.Keystroke() == "ctrl+t" {
		return "ctrl-t"
	}
	if key.Text != "" {
		return key.Text
	}
	return msg.String()
}

func (m *panelModel) clampCursor() {
	if m.panel.Mode == "project-select" {
		m.panel.Cursor = min(m.panel.Cursor, len(m.panel.HandoffProjects)-1)
	} else if m.panel.Mode == "session-select" {
		m.panel.Cursor = min(m.panel.Cursor, len(m.panel.HandoffSessions)-1)
	} else if m.panel.Mode == "conflict-source" {
		m.panel.Cursor = min(m.panel.Cursor, len(m.panel.HandoffConflicts[m.panel.HandoffConflictID])-1)
	} else {
		m.panel.Cursor = min(m.panel.Cursor, len(m.names)-1)
	}
	m.panel.Cursor = max(0, m.panel.Cursor)
}

func (m *panelModel) refreshSettings() bool {
	settings, err := m.app.settings()
	if err != nil {
		m.err = err
		return false
	}
	m.settings = settings
	return true
}

func (m *panelModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 && msg.Height > 0 {
			m.panel.Width, m.panel.Height = msg.Width, msg.Height
		}
	case panelRefreshMsg:
		if msg.ID != m.refreshID {
			return m, nil
		}
		m.refreshCancel = nil
		if !msg.Cancelled {
			for _, update := range msg.Updates {
				m.panel.Records[update.Name] = reconcileQuotaRecord(m.panel.Records[update.Name], update.Record, update.Error)
			}
			m.panel.Due = time.Now().Add(time.Duration(m.interval) * time.Second)
		}
		m.panel.Busy = false
		if m.pendingAction != "" {
			action := m.pendingAction
			m.pendingAction = ""
			return m, m.actionCommand(action)
		}
	case panelUpdateCheckMsg:
		available := bool(msg)
		if m.panel.UpdateAvailable != available {
			m.panel.MenuCursor = 0
		}
		m.panel.UpdateAvailable = available
	case panelActionDoneMsg:
		if msg.Error != nil {
			m.panel.Message = "Action failed: " + msg.Error.Error()
		} else {
			m.panel.Message = msg.Result.Message
		}
		if msg.Result.ExitAction != "" {
			m.action = msg.Result.ExitAction
			return m, tea.Quit
		}
		m.names = m.app.names()
		if m.filter != "" {
			m.names = []string{m.filter}
		}
		if !m.refreshSettings() {
			return m, tea.Quit
		}
		m.panel.Mode = "home"
		m.panel.Cursor, m.panel.MenuCursor, m.panel.Offset = 0, 0, 0
		return m, tea.Batch(m.startRefresh(), m.updateCheckCommand())
	case panelTickMsg:
		now := time.Time(msg)
		commands := []tea.Cmd{panelTick()}
		if !now.Before(m.nextUpdateCheck) {
			m.nextUpdateCheck = now.Add(updateCheckInterval)
			commands = append(commands, m.updateCheckCommand())
		}
		if !m.panel.Busy && !m.panel.Due.IsZero() && !now.Before(m.panel.Due) {
			commands = append(commands, m.startRefresh())
		}
		return m, tea.Batch(commands...)
	case tea.KeyPressMsg:
		m.clampCursor()
		action, err := m.panel.key(m.app, m.names, bubbleKey(msg))
		if err != nil {
			m.panel.Message = err.Error()
		}
		if !m.refreshSettings() {
			return m, tea.Quit
		}
		switch {
		case action == "refresh":
			if !m.panel.Busy {
				return m, m.startRefresh()
			}
		case action == "quit":
			m.action = action
			return m, tea.Quit
		case action == "add", action == "update", strings.HasPrefix(action, "remove:"), strings.HasPrefix(action, "project-handoff:"):
			if m.panel.Busy {
				m.pendingAction = action
				if m.refreshCancel != nil {
					m.refreshCancel()
				}
				return m, nil
			}
			return m, m.actionCommand(action)
		}
	}
	m.clampCursor()
	return m, nil
}

func (m *panelModel) View() tea.View {
	view := tea.NewView(m.panel.render(m.app, m.names, m.settings, time.Now()))
	view.AltScreen = true
	view.WindowTitle = "xswap"
	return view
}

func (a *App) panel(mode, filter string, interval int) (string, error) {
	if !interactive() {
		return "", errors.New("the panel needs an interactive terminal; use xswap watch --once or xswap limits")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	model, err := newPanelModel(ctx, a, mode, filter, interval)
	if err != nil {
		return "", err
	}
	a.ensureDaemon()
	result, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	if err != nil && !errors.Is(err, context.Canceled) {
		return "", err
	}
	final, ok := result.(*panelModel)
	if !ok {
		return "", errors.New("panel returned an unexpected model")
	}
	if final.err != nil {
		return "", final.err
	}
	return final.action, nil
}
