package main

import (
	"context"
	"errors"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
)

type panelTickMsg time.Time
type panelUpdateCheckMsg bool
type panelRefreshMsg []panelUpdate

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
		app:      app,
		panel:    Panel{Mode: mode, Filter: filter, Records: map[string]Record{}, Width: 120, Height: 30, Busy: true},
		names:    names,
		settings: settings,
		filter:   filter,
		interval: interval,
		ctx:      ctx,
	}, nil
}

func panelTick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(now time.Time) tea.Msg { return panelTickMsg(now) })
}

func (m *panelModel) refreshCommand() tea.Cmd {
	names := append([]string{}, m.names...)
	return func() tea.Msg {
		updates := make([]panelUpdate, 0, len(names)+1)
		for _, name := range names {
			if m.ctx.Err() != nil {
				return panelRefreshMsg(updates)
			}
			record, err := m.app.readLimits(m.ctx, name)
			message := ""
			if err != nil {
				message = err.Error()
			}
			updates = append(updates, panelUpdate{Name: name, Record: record, Error: message})
		}
		return panelRefreshMsg(updates)
	}
}

func (m *panelModel) updateCheckCommand() tea.Cmd {
	return func() tea.Msg { return panelUpdateCheckMsg(m.app.checkUpdate(m.ctx)) }
}

func (m *panelModel) Init() tea.Cmd {
	m.nextUpdateCheck = time.Now().Add(updateCheckInterval)
	return tea.Batch(panelTick(), m.refreshCommand(), m.updateCheckCommand())
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
		for _, update := range msg {
			m.panel.Records[update.Name] = reconcileQuotaRecord(m.panel.Records[update.Name], update.Record, update.Error)
		}
		m.panel.Busy = false
		m.panel.Due = time.Now().Add(time.Duration(m.interval) * time.Second)
	case panelUpdateCheckMsg:
		available := bool(msg)
		if m.panel.UpdateAvailable != available {
			m.panel.MenuCursor = 0
		}
		m.panel.UpdateAvailable = available
	case panelTickMsg:
		now := time.Time(msg)
		commands := []tea.Cmd{panelTick()}
		if !now.Before(m.nextUpdateCheck) {
			m.nextUpdateCheck = now.Add(updateCheckInterval)
			commands = append(commands, m.updateCheckCommand())
		}
		if !m.panel.Busy && !m.panel.Due.IsZero() && !now.Before(m.panel.Due) {
			m.panel.Busy = true
			commands = append(commands, m.refreshCommand())
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
				m.panel.Busy = true
				return m, m.refreshCommand()
			}
		case action == "quit", action == "add", action == "update", strings.HasPrefix(action, "remove:"), strings.HasPrefix(action, "project-handoff:"):
			m.action = action
			return m, tea.Quit
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
