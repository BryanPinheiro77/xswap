package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSessionTimeFilterBoundariesAndOrdering(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	sessions := []codexSession{
		{ID: "week-before", Updated: now.Add(-7*24*time.Hour - time.Nanosecond)},
		{ID: "day-before", Updated: now.Add(-24*time.Hour - time.Nanosecond)},
		{ID: "week-boundary", Updated: now.Add(-7 * 24 * time.Hour)},
		{ID: "day-boundary", Updated: now.Add(-24 * time.Hour)},
		{ID: "future", Updated: now.Add(time.Hour)},
		{ID: "now", Updated: now},
		{ID: "unknown"},
	}
	before := append([]codexSession(nil), sessions...)
	for _, tc := range []struct {
		filter sessionTimeFilter
		want   []string
	}{
		{sessionFilterDay, []string{"future", "now", "day-boundary"}},
		{sessionFilterWeek, []string{"future", "now", "day-boundary", "day-before", "week-boundary"}},
		{sessionFilterAll, []string{"future", "now", "day-boundary", "day-before", "week-boundary", "week-before", "unknown"}},
	} {
		t.Run(tc.filter.label(), func(t *testing.T) {
			got := filterSessions(sessions, tc.filter, now)
			var ids []string
			for _, session := range got {
				ids = append(ids, session.ID)
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("got %v, want %v", ids, tc.want)
			}
		})
	}
	if !reflect.DeepEqual(sessions, before) {
		t.Fatal("filter mutated the source snapshot")
	}
	var many []codexSession
	for i := 0; i < 25; i++ {
		many = append(many, codexSession{ID: fmt.Sprintf("%02d", i), Updated: now.Add(time.Duration(i) * time.Hour)})
	}
	got := filterSessions(many, sessionFilterRecent, now)
	if len(got) != 20 || got[0].ID != "24" || got[19].ID != "05" {
		t.Fatal("recent must select the latest 20 by activity", got)
	}
}

func TestSessionPickerFiltersSelectionAndEmptyRecovery(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	p := Panel{Mode: "session-select", Width: 80, Height: 24, Records: map[string]Record{}}
	p.useHandoffPlan(projectHandoffPlan{Project: t.TempDir(), Sessions: []codexSession{
		{ID: "old", Account: "default", Preview: "older conversation", Updated: now.Add(-8 * 24 * time.Hour)},
		{ID: "week", Account: "default", Preview: "weekly conversation", Updated: now.Add(-48 * time.Hour)},
		{ID: "day", Account: "default", Preview: "daily conversation", Updated: now.Add(-time.Hour)},
	}})
	p.HandoffFilterTime = now
	names := a.names()
	s, _ := a.settings()
	if len(p.selectedSessions()) != 3 || p.HandoffTimeFilter != sessionFilterAll {
		t.Fatal("picker must initially include all sessions")
	}
	p.key(a, names, "2")
	if p.Mode != "session-select" || len(p.HandoffSessions) != 1 || !p.HandoffSelected["day"] || p.HandoffSelected["old"] || p.Cursor != 0 || p.Offset != 0 {
		t.Fatal("day filter selected hidden sessions", p)
	}
	p.key(a, names, " ")
	if len(p.selectedSessions()) != 0 {
		t.Fatal("Space did not toggle a filtered session")
	}
	p.key(a, names, "a")
	if len(p.selectedSessions()) != 1 {
		t.Fatal("select all included hidden sessions")
	}
	p.key(a, names, "3")
	if len(p.selectedSessions()) != 2 || p.HandoffSelected["old"] {
		t.Fatal("changing filters did not select every matching session")
	}
	p.key(a, names, "4")
	if len(p.selectedSessions()) != 3 || p.HandoffSessions[0].ID != "day" {
		t.Fatal("All sessions did not restore the full ordered list")
	}
	// An empty result must keep every recovery control available at minimum size.
	p.HandoffFilterTime = now.Add(30 * 24 * time.Hour)
	p.key(a, names, "2")
	for _, key := range []string{"down", "up", " ", "a", "enter"} {
		if _, err := p.key(a, names, key); err != nil || p.Cursor < 0 || len(p.selectedSessions()) != 0 || p.Mode != "session-select" {
			t.Fatal("empty result broke navigation or continued handoff", key, err)
		}
	}
	for _, width := range []int{50, 80, 120} {
		p.Width = width
		p.Height = 20
		view := stripANSI(p.render(a, names, s, now))
		for _, text := range []string{"Filter: Last 24 hours", "4 All", "No sessions match", "Press 4 for All sessions", "esc Back"} {
			if !strings.Contains(view, text) {
				t.Fatalf("width %d missing %q:\n%s", width, text, view)
			}
		}
	}
	p.key(a, names, "4")
	if len(p.selectedSessions()) != 3 || p.Cursor != 0 || p.Offset != 0 || p.Message != "" {
		t.Fatal("empty result could not recover to All sessions")
	}
	p.key(a, names, "esc")
	if p.Mode != "home" {
		t.Fatal("cannot go back from filtered picker")
	}
}

func TestFilteredProjectHandoffCopiesOnlyMatches(t *testing.T) {
	for _, value := range []string{"24h", "7d", "all", ""} {
		t.Run(value, func(t *testing.T) {
			a := fixture(t)
			ready(t, a, "work")
			project := t.TempDir()
			now := time.Now()
			oldID := "11111111-1111-4111-8111-111111111111"
			newID := "22222222-2222-4222-8222-222222222222"
			oldPath := writeTestSession(t, a.DefaultHome, "2026/09/17", oldID, project, "old")
			writeTestSession(t, a.DefaultHome, "2026/09/18", newID, project, "recent activity on an older session")
			if err := os.Chtimes(oldPath, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour)); err != nil {
				t.Fatal(err)
			}
			indexed := []string{}
			a.SessionIndexer = func(_ string, id string) error { indexed = append(indexed, id); return nil }
			args := []string{"project", "switch", "work", "--path", project, "--yes"}
			if value != "" {
				args = append(args, "--sessions", value)
			}
			if err := a.run(args); err != nil {
				t.Fatal(err)
			}
			home, _ := a.profile("work")
			copied, err := sessionsInProject(home, project)
			want := 1
			if value == "all" || value == "" {
				want = 2
			}
			if err != nil || len(copied) != want || len(indexed) != want {
				t.Fatal("incorrect sessions transferred", copied, indexed, err)
			}
			if want == 1 && copied[0].ID != newID {
				t.Fatal("filtered by creation time instead of last activity")
			}
			if a.selected() != "default" {
				t.Fatal("project filter changed the global account")
			}
		})
	}
}

func TestSessionFilterRejectsInvalidAndEmptySelectionsWithoutChanges(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	path := writeTestSession(t, a.DefaultHome, "2026/09/17", "11111111-1111-4111-8111-111111111111", project, "old")
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"project", "switch", "work", "--path", project, "--sessions", "24h", "--yes"},
		{"project", "switch", "work", "--path", project, "--sessions", "invalid", "--yes"},
		{"project", "switch", "work", "--sessions"},
		{"project", "switch", "work", "--sessions", ""},
		{"project", "use", "work", "--sessions", "all"},
		{"list", "--sessions", "all"},
	} {
		if err := a.run(args); err == nil {
			t.Fatal("accepted an invalid or empty filter selection", args)
		}
		if exists(filepath.Join(project, projectAccountFile)) || a.selected() != "default" {
			t.Fatal("failed filter changed account selection")
		}
	}
	home, _ := a.profile("work")
	if copied, err := sessionsInProject(home, project); err != nil || len(copied) != 0 {
		t.Fatal("failed filter copied conversations", copied, err)
	}
}

func TestTimeFilteredHandoffOmitsExcludedConflicts(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	project := t.TempDir()
	id := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	left, right := divergentSession(t, a, project, id)
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, path := range []string{left, right} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	recentID := "22222222-2222-4222-8222-222222222222"
	writeTestSession(t, a.DefaultHome, "2026/09/18", recentID, project, "recent")
	plan, err := a.planFilteredProjectHandoff(project, "work", sessionFilterDay, time.Now())
	if err != nil || len(plan.Sessions) != 1 || plan.Sessions[0].ID != recentID || len(plan.Conflicts) != 0 {
		t.Fatal("excluded conflict blocked the filtered handoff", plan, err)
	}
	if _, err := a.planFilteredProjectHandoff(project, "work", sessionFilterAll, time.Now()); err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatal("All sessions bypassed conflict protection", err)
	}
}

func TestSessionPickerFilterControlsRemainVisibleWhileScrolling(t *testing.T) {
	a := fixture(t)
	now := time.Now()
	var sessions []codexSession
	for i := 0; i < 25; i++ {
		sessions = append(sessions, codexSession{ID: fmt.Sprintf("%02d", i), Account: "default", Preview: fmt.Sprintf("Conversation %d", i), Updated: now.Add(-time.Duration(i) * time.Hour)})
	}
	p := Panel{Mode: "session-select", Width: 80, Height: 20, Records: map[string]Record{}}
	p.useHandoffPlan(projectHandoffPlan{Project: t.TempDir(), Sessions: sessions})
	p.key(a, a.names(), "1")
	for i := 0; i < 25; i++ {
		p.key(a, a.names(), "down")
	}
	s, _ := a.settings()
	view := stripANSI(p.render(a, a.names(), s, now))
	rows := strings.Split(view, "\n")
	if len(p.selectedSessions()) != 20 || !strings.Contains(rows[2], "Recent sessions (latest 20)") || !strings.Contains(rows[2], "20/25 shown") || !strings.Contains(rows[3], "4 All") || !strings.Contains(view, "Conversation 19") {
		t.Fatal("filter controls or current conversation disappeared while scrolling", view)
	}
	p.key(a, a.names(), "4")
	if len(p.selectedSessions()) != 25 || p.Cursor != 0 || p.Offset != 0 {
		t.Fatal("All sessions did not restore every hidden conversation")
	}
}
