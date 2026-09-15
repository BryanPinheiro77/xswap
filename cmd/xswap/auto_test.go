package main

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuotaEligibilityRejectsIncompleteAndNonfiniteValues(t *testing.T) {
	now := time.Now().Unix()
	for _, tc := range []struct {
		name   string
		mutate func(*Record)
	}{
		{"missing bucket", func(r *Record) { r.Limits = Limits{} }},
		{"missing windows", func(r *Record) { r.Limits.Main.Primary = nil; r.Limits.Main.Secondary = nil }},
		{"missing usage", func(r *Record) { r.Limits.Main.Primary.Used = nil }},
		{"missing reset", func(r *Record) { r.Limits.Main.Primary.Resets = nil }},
		{"negative usage", func(r *Record) { r.Limits.Main.Primary.Used = pointer(-1.0) }},
		{"NaN", func(r *Record) { r.Limits.Main.Primary.Used = pointer(math.NaN()) }},
		{"infinity", func(r *Record) { r.Limits.Main.Primary.Used = pointer(math.Inf(1)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := quota(10, 20, now)
			tc.mutate(&r)
			if _, valid := quotaScore(r, now, 120); valid {
				t.Fatal("accepted ineligible quota")
			}
		})
	}
}

func TestAutoSwitchDoesNotSelectCandidateWithPendingLogin(t *testing.T) {
	a := fixture(t)
	if _, err := a.create("pending"); err != nil {
		t.Fatal(err)
	}
	s, err := a.settings()
	if err != nil {
		t.Fatal(err)
	}
	s.Auto.Enabled = true
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), s); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	records := map[string]Record{"default": quota(95, 80, now), "pending": quota(10, 20, now)}
	result, err := a.evaluate(records, "default", false, false)
	if err != nil || result.Switched || result.Target != "" || a.selected() != "default" {
		t.Fatal(result, err)
	}
	if !strings.Contains(result.Message, "login unavailable") {
		t.Fatal(result.Message)
	}
}

func TestAutoViewExplainsEligibilityAndRecentSwitches(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	if err := a.setEnabled("work", false); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	p := Panel{Mode: "auto", Width: 120, Height: 30, Records: map[string]Record{"default": quota(40, 60, now.Unix()), "work": quota(10, 20, now.Unix())}}
	state := AutoState{Checked: now.Unix(), Message: "Selection held.", History: []SwitchEvent{{At: now.Unix(), From: "work", To: "default"}}}
	if err := writeJSON(filepath.Join(a.Root, "auto-state.json"), state); err != nil {
		t.Fatal(err)
	}
	s, err := a.settings()
	if err != nil {
		t.Fatal(err)
	}
	rendered := stripANSI(p.render(a, a.names(), s, now))
	for _, text := range []string{"Auto-switch off", "monitor stopped", "60% used", "disabled", "active", "Recent switches", "work → default", "Selection held."} {
		if !strings.Contains(rendered, text) {
			t.Fatal("missing auto explanation", text)
		}
	}
}
