package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type mode int

const (
	menuMode mode = iota
	inputMode
	confirmMode
)

type tickMsg time.Time
type subprocessDoneMsg struct{ err error }

type model struct {
	mode       mode
	cursor     int
	width      int
	height     int
	input      string
	saved      string
	message    string
	ticks      int
	quitting   bool
	subprocess func() *exec.Cmd
}

var menu = []string{"Rename account…", "Run terminal input probe…", "Confirm action…", "Quit"}

func initialModel() model {
	return model{subprocess: probeCommand}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return tickMsg(now) })
}

func (m model) Init() tea.Cmd { return tick() }

func keyMatches(key tea.KeyPressMsg, values ...string) bool {
	name, stroke := key.String(), key.Keystroke()
	for _, value := range values {
		if value == name || value == stroke || value == key.Key().Text {
			return true
		}
	}
	return false
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.ticks++
		return m, tick()
	case subprocessDoneMsg:
		if msg.err != nil {
			m.message = "subprocess failed: " + msg.err.Error()
		} else {
			m.message = "terminal input returned to Bubble Tea"
		}
	case tea.KeyPressMsg:
		key := msg.Key()
		if keyMatches(msg, "ctrl+c") || (m.mode == menuMode && keyMatches(msg, "q")) {
			m.quitting = true
			return m, tea.Quit
		}
		switch m.mode {
		case inputMode:
			switch {
			case keyMatches(msg, "esc"):
				m.mode, m.input = menuMode, ""
			case keyMatches(msg, "enter"):
				m.saved, m.message = m.input, "display name saved"
				m.mode, m.input = menuMode, ""
			case keyMatches(msg, "backspace", "delete"):
				if runes := []rune(m.input); len(runes) > 0 {
					m.input = string(runes[:len(runes)-1])
				}
			case key.Text != "":
				m.input += key.Text
			}
		case confirmMode:
			if keyMatches(msg, "enter", "y", "Y") {
				m.message, m.mode = "action confirmed once", menuMode
			} else if keyMatches(msg, "esc", "n", "N") {
				m.message, m.mode = "action cancelled", menuMode
			}
		case menuMode:
			switch {
			case keyMatches(msg, "up", "k"):
				m.cursor = (m.cursor + len(menu) - 1) % len(menu)
			case keyMatches(msg, "down", "j"):
				m.cursor = (m.cursor + 1) % len(menu)
			case keyMatches(msg, "enter"):
				switch m.cursor {
				case 0:
					m.mode, m.input = inputMode, m.saved
				case 1:
					return m, tea.ExecProcess(m.subprocess(), func(err error) tea.Msg {
						return subprocessDoneMsg{err: err}
					})
				case 2:
					m.mode = confirmMode
				case 3:
					m.quitting = true
					return m, tea.Quit
				}
			}
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	var content strings.Builder
	fmt.Fprintf(&content, "xswap Bubble Tea v2 prototype · %dx%d · ticks %d\n\n", m.width, m.height, m.ticks)
	switch m.mode {
	case inputMode:
		fmt.Fprintf(&content, "Display name: %s\n\nEnter saves · Esc cancels", m.input)
	case confirmMode:
		content.WriteString("Confirm the action?\n\nEnter / y confirms · Esc cancels")
	default:
		for index, item := range menu {
			marker := "  "
			if index == m.cursor {
				marker = "› "
			}
			fmt.Fprintln(&content, marker+item)
		}
		if m.saved != "" {
			fmt.Fprintln(&content, "\nUnicode value:", m.saved)
		}
	}
	if m.message != "" {
		fmt.Fprintln(&content, "\n"+m.message)
	}
	view := tea.NewView(content.String())
	view.AltScreen = true
	return view
}

func probeCommand() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "set /p answer=probe input:  & echo child received input")
	}
	return exec.Command("sh", "-c", "printf 'probe input: '; IFS= read -r answer; printf 'child received: %s\\n' \"$answer\"")
}

func main() {
	if _, err := tea.NewProgram(initialModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
