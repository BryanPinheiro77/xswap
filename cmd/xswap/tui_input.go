package main

import (
	"os"
	"path/filepath"
	"unicode"
)

func appendPanelInput(current, text string, limit int) string {
	runes := []rune(current)
	for _, value := range text {
		if unicode.IsControl(value) || len(runes) >= limit {
			continue
		}
		runes = append(runes, value)
	}
	return string(runes)
}

func (p *Panel) openHandoffDestination(a *App, names []string) {
	p.Mode = "project-switch"
	p.Cursor = 0
	sources := p.selectedSourceAccounts()
	if len(sources) == 1 {
		for index, name := range names {
			if name != sources[0] {
				p.Cursor = index
				break
			}
		}
	} else {
		for index, name := range names {
			if name == a.selected() {
				p.Cursor = index
				break
			}
		}
	}
	p.Offset = 0
}

func (p *Panel) key(a *App, names []string, key string) (string, error) {
	if key == "ctrl-c" || (key == "q" && p.Mode != "rename-input") {
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
		if p.Mode == "conflict-source" {
			p.Mode = "session-select"
			p.HandoffConflictID = ""
			p.Cursor = 0
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
			return handoffAction(p.HandoffProject, p.Pending, p.selectedSessions(), p.HandoffResolutions), nil
		}
		return "", nil
	}
	if p.Mode == "rename-input" {
		if key == "backspace" || key == "delete" {
			if runes := []rune(p.Input); len(runes) > 0 {
				p.Input = string(runes[:len(runes)-1])
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
		if key != "up" && key != "down" {
			p.Input = appendPanelInput(p.Input, key, 64)
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
		} else if p.Mode == "conflict-source" {
			p.Cursor = min(p.Cursor+1, len(p.HandoffConflicts[p.HandoffConflictID])-1)
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
		} else if p.Mode == "conflict-source" {
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
			} else if len(names) < 2 {
				p.Message = "Add another account before continuing these conversations."
			} else if p.beginConflictResolution() {
				return "", nil
			} else {
				p.openHandoffDestination(a, names)
			}
		} else if p.Mode == "conflict-source" {
			copies := p.HandoffConflicts[p.HandoffConflictID]
			if len(copies) == 0 {
				p.Message = "No account copies are available for this conflict."
				p.Mode = "session-select"
			} else {
				p.HandoffResolutions[p.HandoffConflictID] = copies[p.Cursor].Account
				p.HandoffConflictID = ""
				if !p.beginConflictResolution() {
					p.openHandoffDestination(a, names)
				}
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
			plan, err := a.planResolvedProjectHandoff(p.HandoffProject, names[p.Cursor], p.HandoffSelected, p.HandoffResolutions)
			if err != nil {
				p.Message = err.Error()
			} else {
				p.HandoffUnmanaged = map[string]bool{}
				for _, session := range plan.Unmanaged {
					p.HandoffUnmanaged[session.ID] = true
				}
				p.Pending = names[p.Cursor]
				p.HandoffProject = plan.Project
				p.HandoffSources = plan.Sources
				p.HandoffManaged = len(plan.Managed)
				p.HandoffArchives = conflictArchiveCount(plan)
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
