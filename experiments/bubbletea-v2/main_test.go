package main

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func press(text string, code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: text, Code: code})
}

func update(t *testing.T, current model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	next, command := current.Update(msg)
	result, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	return result, command
}

func TestNavigationInputUnicodeAndConfirmation(t *testing.T) {
	m := initialModel()
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 90, Height: 40})
	if m.width != 90 || m.height != 40 {
		t.Fatal("resize was not retained", m.width, m.height)
	}
	m, _ = update(t, m, press("", tea.KeyEnter))
	if m.mode != inputMode {
		t.Fatal("Enter did not open text input")
	}
	for _, value := range []string{"W", "o", "r", "k", " ", "🚀"} {
		m, _ = update(t, m, press(value, []rune(value)[0]))
	}
	m, _ = update(t, m, press("", tea.KeyBackspace))
	m, _ = update(t, m, press("✨", '✨'))
	m, _ = update(t, m, press("", tea.KeyEnter))
	if m.saved != "Work ✨" || m.mode != menuMode {
		t.Fatal("Unicode input was not saved", m.saved, m.mode)
	}
	m.cursor = 2
	m, _ = update(t, m, press("", tea.KeyEnter))
	m, _ = update(t, m, press("", tea.KeyEnter))
	if m.message != "action confirmed once" || m.mode != menuMode {
		t.Fatal("single Enter did not confirm", m.message, m.mode)
	}
	view := m.View().Content
	for _, expected := range []string{"90x40", "Work ✨", "action confirmed once"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view omitted %q:\n%s", expected, view)
		}
	}
}

func TestSubprocessCommandAndTerminalReturnMessage(t *testing.T) {
	m := initialModel()
	m.cursor = 1
	m, command := update(t, m, press("", tea.KeyEnter))
	if command == nil {
		t.Fatal("terminal subprocess produced no Bubble Tea command")
	}
	m, _ = update(t, m, subprocessDoneMsg{})
	if m.message != "terminal input returned to Bubble Tea" {
		t.Fatal("success did not restore panel state", m.message)
	}
	m, _ = update(t, m, subprocessDoneMsg{err: errors.New("exit 1")})
	if !strings.Contains(m.message, "exit 1") {
		t.Fatal("subprocess error was hidden", m.message)
	}
}

func TestEscapeCancelsInputWithoutSaving(t *testing.T) {
	m := initialModel()
	m.saved = "Personal"
	m, _ = update(t, m, press("", tea.KeyEnter))
	m, _ = update(t, m, press("X", 'X'))
	m, _ = update(t, m, press("", tea.KeyEsc))
	if m.mode != menuMode || m.saved != "Personal" || m.input != "" {
		t.Fatal("Escape changed saved input", m)
	}
}
