package main

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func bubblePress(text string, code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: text, Code: code})
}

func updatePanelModel(t *testing.T, model *panelModel, message tea.Msg) (*panelModel, tea.Cmd) {
	t.Helper()
	next, command := model.Update(message)
	result, ok := next.(*panelModel)
	if !ok {
		t.Fatalf("panel update returned %T", next)
	}
	return result, command
}

func TestBubbleTeaPanelResizeNavigationAndUnicodeInput(t *testing.T) {
	a := fixture(t)
	model, err := newPanelModel(context.Background(), a, "home", "", 60)
	if err != nil {
		t.Fatal(err)
	}
	model, _ = updatePanelModel(t, model, tea.WindowSizeMsg{Width: 90, Height: 40})
	if model.panel.Width != 90 || model.panel.Height != 40 {
		t.Fatal("window size was not retained", model.panel.Width, model.panel.Height)
	}
	model.panel.MenuCursor = 5 // Rename account…
	model, _ = updatePanelModel(t, model, bubblePress("", tea.KeyEnter))
	if model.panel.Mode != "rename" {
		t.Fatal("Enter did not open account selection", model.panel.Mode)
	}
	model, _ = updatePanelModel(t, model, bubblePress("", tea.KeyEnter))
	if model.panel.Mode != "rename-input" {
		t.Fatal("Enter did not open rename input", model.panel.Mode)
	}
	for _, value := range []string{"W", "o", "r", "k", " ", "q", "✨"} {
		model, _ = updatePanelModel(t, model, bubblePress(value, []rune(value)[0]))
	}
	model, _ = updatePanelModel(t, model, bubblePress("", tea.KeyEnter))
	settings, err := a.settings()
	if err != nil || settings.DisplayNames["default"] != "Work q✨" {
		t.Fatal("Unicode display name was not saved", settings.DisplayNames, err)
	}
	view := model.View()
	if !view.AltScreen || !strings.Contains(view.Content, "xswap") {
		t.Fatal("panel view did not retain alternate-screen rendering")
	}
}

func TestBubbleTeaPanelIgnoresZeroWindowSize(t *testing.T) {
	a := fixture(t)
	model, err := newPanelModel(context.Background(), a, "home", "", 60)
	if err != nil {
		t.Fatal(err)
	}
	model, _ = updatePanelModel(t, model, tea.WindowSizeMsg{})
	if model.panel.Width != 120 || model.panel.Height != 30 {
		t.Fatal("zero-sized pseudo-terminal replaced safe defaults", model.panel.Width, model.panel.Height)
	}
}
